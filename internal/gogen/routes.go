package gogen

import (
	"fmt"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/sqlgen"
)

// handlers returns handlers.go: one function per route and the route
// table.
func (g *gen) handlers() string {
	var b strings.Builder
	b.WriteString(header)
	names := make([]string, len(g.prog.Routes))
	taken := map[string]bool{}
	var funcs strings.Builder
	for i, r := range g.prog.Routes {
		if r.Origin == ir.FromBuiltin {
			names[i] = "vibe.Healthz"
			continue
		}
		name := handlerName(r)
		for taken[name] {
			name += "_"
		}
		taken[name] = true
		names[i] = name
		funcs.WriteString("\n" + g.handler(r, name))
	}
	b.WriteString("// routes returns every route of the app, crud routes included.\nfunc routes() []vibe.Route {\n\treturn []vibe.Route{\n")
	for i, r := range g.prog.Routes {
		fmt.Fprintf(&b, "\t\t{Method: %q, Path: %q, Handle: %s},\n", r.Method, r.Path, names[i])
	}
	b.WriteString("\t}\n}\n")
	b.WriteString(funcs.String())
	return b.String()
}

// handlerName names the handler of a route after its method and path:
// GET /books/{id} is handleGetBooksID.
func handlerName(r *ir.Route) string {
	var b strings.Builder
	b.WriteString("handle" + pascal(strings.ToLower(r.Method)))
	for _, seg := range strings.Split(r.Path, "/") {
		seg = strings.Trim(seg, "{}")
		var word strings.Builder
		for _, c := range seg {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
				word.WriteRune(c)
			} else {
				word.WriteByte('_')
			}
		}
		b.WriteString(pascal(word.String()))
	}
	return b.String()
}

func (g *gen) handler(r *ir.Route, name string) string {
	f := newBody(g)
	f.route = r
	desc := r.Method + " " + r.Path
	switch r.Origin {
	case ir.FromCrud:
		desc += ", crud " + r.Model.Name + " " + r.Op
		if r.Owner != nil {
			g.unsupported(r.Pos, "crud with owner")
		}
		g.crud(f, r)
	default:
		f.markUsed(r.Body, r.Value)
		f.params(r)
		if r.Value != nil {
			f.answer(r.Value)
		} else {
			f.block(r.Body)
			if !terminates(r.Body) {
				f.line("return c.Empty()")
			}
		}
	}
	return fmt.Sprintf("// %s answers %s, declared at %s.\nfunc %s(c *vibe.Ctx) error {\n%s}\n", name, desc, where(r.Pos), name, f.w.String())
}

// terminates reports whether a block always ends in return or fail.
func terminates(b *ir.Block) bool {
	if b == nil || len(b.Stmts) == 0 {
		return false
	}
	return stmtTerminates(b.Stmts[len(b.Stmts)-1])
}

func stmtTerminates(s ir.Stmt) bool {
	switch s := s.(type) {
	case *ir.Return, *ir.Fail:
		return true
	case *ir.Block:
		return terminates(s)
	case *ir.If:
		return s.Else != nil && terminates(s.Then) && stmtTerminates(s.Else)
	}
	return false
}

// params writes the code that reads the params of a route into locals.
func (f *body) params(r *ir.Route) {
	var query, body []*ir.Param
	for _, p := range r.Params {
		switch p.In {
		case ir.InPath:
			f.pathParam(p)
		case ir.InQuery:
			query = append(query, p)
		case ir.InBody:
			body = append(body, p)
		}
	}
	switch {
	case r.Method == "POST" || r.Method == "PATCH":
		// A body is read even without params, so a field the route does
		// not take is reported.
		f.line("in, err := c.Body()")
		f.failOn()
		f.inputs(body)
	case len(query) > 0:
		f.line("in := c.Query()")
		f.inputs(query)
	default:
		return
	}
	f.line("if err := in.Err(); err != nil {\nreturn err\n}")
}

func (f *body) pathParam(p *ir.Param) {
	target := "_"
	if f.used[p.Var] {
		target = f.local(p.Var)
	}
	if p.Type.Kind == ir.ID {
		f.line("%s, err := c.PathID(%q)", target, p.Name)
		f.failOn()
		return
	}
	if target != "_" {
		f.line("%s := c.PathText(%q)", target, p.Name)
	}
}

func (f *body) inputs(params []*ir.Param) {
	for _, p := range params {
		x := f.read(p)
		if f.used[p.Var] {
			f.line("%s := %s", f.local(p.Var), x)
		} else {
			f.line("_ = %s", x)
		}
	}
}

// read returns the call that reads param p from in.
func (f *body) read(p *ir.Param) string {
	kind := inputKind(p.Type)
	if kind == "" {
		f.g.unsupported(p.Pos, "a param of type "+p.Type.String())
	}
	t := goType(elem(p.Type))
	switch {
	case p.Type.Kind == ir.Optional && p.Default != nil && p.Default.Value != nil:
		return fmt.Sprintf("vibe.OptDef(in, %q, %s, %s)", p.Name, kind, constant(withType(p.Default, elem(p.Type))))
	case p.Type.Kind == ir.Optional:
		return fmt.Sprintf("vibe.Opt[%s](in, %q, %s)", t, p.Name, kind)
	case p.Default != nil:
		return fmt.Sprintf("vibe.Def(in, %q, %s, %s)", p.Name, kind, constant(withType(p.Default, p.Type)))
	}
	return fmt.Sprintf("vibe.Req[%s](in, %q, %s)", t, p.Name, kind)
}

// withType returns k with type t, so its Go constant has the type of the
// place it fills.
func withType(k *ir.Const, t *ir.Type) *ir.Const {
	out := *k
	out.T = t
	return &out
}

// crud writes the body of a crud route.
func (g *gen) crud(f *body, r *ir.Route) {
	m := r.Model
	n := m.Name
	find := func() {
		key := "c.PathText(" + fmt.Sprintf("%q", r.Key.Name) + ")"
		f.line("r, err := find%sBy%s(c, %s)", n, g.fields[r.Key], key)
		f.failOn()
	}
	switch r.Op {
	case "list":
		list := g.constSQL("sql"+n+"List", sqlgen.List(m), "lists "+n+" records, newest first.")
		f.line(`in := c.Query()
limit := vibe.Def(in, "limit", vibe.AsInt, int64(50))
offset := vibe.Def(in, "offset", vibe.AsInt, int64(0))
if limit < 0 || limit > 1000 {
	in.Invalid("limit", "must be between 0 and 1000")
}
if offset < 0 {
	in.Invalid("offset", "must be at least 0")
}
if err := in.Err(); err != nil {
	return err
}
out, err := vibe.All(c, %s, scan%s, limit, offset)
if err != nil {
	return err
}
return c.OK(out)`, list, n)
	case "get":
		find()
		f.line("return c.OK(r)")
	case "create":
		f.line("in, err := c.Body()")
		f.failOn()
		f.line("in.ReadOnly(\"id\", \"created_at\", \"updated_at\")")
		f.line("r := &%s{}", n)
		for _, p := range r.Params {
			field := m.Field(p.Name)
			x := f.read(withParamType(p, field.Type))
			if field.Type.Kind == ir.Slug {
				// A slug is generated when it is not given.
				x = "vibe.Deref(" + f.read(p) + ")"
			}
			f.line("r.%s = %s", g.fields[field], x)
		}
		f.line(`in.Done()
r, err = insert%s(c, r, &in.Errors)
if err != nil {
	return err
}
return c.Created(r)`, n)
	case "update":
		find()
		f.line("in, err := c.Body()")
		f.failOn()
		f.line("in.ReadOnly(\"id\", \"created_at\", \"updated_at\")")
		for _, p := range r.Params[1:] {
			field := m.Field(p.Name)
			read := withParamType(p, field.Type)
			f.line("if in.Has(%q) {\nr.%s = %s\n}", p.Name, g.fields[field], f.read(read))
		}
		f.line(`in.Done()
r, err = update%s(c, r, &in.Errors)
if err != nil {
	return err
}
return c.OK(r)`, n)
	case "delete":
		find()
		f.line("if err := delete%s(c, r); err != nil {\nreturn err\n}\nreturn c.Empty()", n)
	default:
		panic("gogen: crud op " + r.Op)
	}
}

// withParamType returns p reading a value of the field's type t: a
// required field as a value, so null is "is required", a field with a
// default as a value, and an optional one as a value or null.
func withParamType(p *ir.Param, t *ir.Type) *ir.Param {
	out := *p
	out.Type = t
	return &out
}
