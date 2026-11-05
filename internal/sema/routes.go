package sema

import (
	"slices"
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/token"
)

// typeAliases maps type names from other languages to GoVibe types.
var typeAliases = map[string]string{
	"Integer": "Int", "Long": "Int", "String": "Text", "Str": "Text", "Boolean": "Bool",
	"Double": "Float", "Decimal": "Float", "Number": "Float", "DateTime": "Time", "Date": "Time",
	"Timestamp": "Time", "UUID": "ID", "Uuid": "ID",
}

// resolveType converts a parameter or result type.
func (c *checker) resolveType(f *token.File, t ast.Type) *ir.Type {
	switch t := t.(type) {
	case *ast.NamedType:
		name := t.Name.Name
		if k, ok := baseTypes[name]; ok {
			return ir.Basic(k)
		}
		if m, ok := c.models[name]; ok {
			return ir.ModelOf(m.ir)
		}
		if name == "Slug" {
			c.errorAt(f, diag.V208, t, "'Slug'; Slug(field) is only a model field type, use Text")
			return ir.Basic(ir.Invalid)
		}
		d := c.diagAt(f, diag.V208, t, quote(name))
		candidates := append(c.modelNames(), "Int", "Float", "Text", "Bool", "Time", "Money", "Email", "ID")
		s, ok := typeAliases[name]
		if !ok {
			s, ok = diag.Suggest(name, candidates)
		}
		if ok {
			d = d.WithFix("Use "+quote(s), diag.Replace(f, t.Pos(), t.End(), s))
		}
		c.add(d)
	case *ast.ListType:
		return ir.ListOf(c.resolveType(f, t.Elem))
	case *ast.OptionalType:
		return ir.OptionalOf(c.resolveType(f, t.Elem))
	}
	return ir.Basic(ir.Invalid)
}

// params converts a parameter list. Names in taken, such as path
// parameters, are already used.
func (c *checker) params(f *token.File, ps []*ast.Param, in ir.Source, owner string, taken map[string]bool) []*ir.Param {
	var out []*ir.Param
	for _, p := range ps {
		name := p.Name.Name
		if taken[name] {
			c.errorAt(f, diag.V204, p.Name, "parameter "+quote(name), owner)
			continue
		}
		taken[name] = true
		param := &ir.Param{Name: name, In: in, Pos: pos(f, p.Pos())}
		if p.Type != nil {
			param.Type = c.resolveType(f, p.Type)
		}
		if p.Default != nil {
			param.Default = c.constant(f, p.Default, param.Type, "the default of parameter "+name)
			if param.Type == nil {
				switch {
				case param.Default == nil:
					param.Type = ir.Basic(ir.Invalid)
				case param.Default.T.Kind == ir.None:
					c.errorAt(f, diag.V301, p.Default, "parameter "+name+" = none needs a type, as in "+name+": Text? = none")
					param.Type = ir.Basic(ir.Invalid)
				default:
					param.Type = param.Default.T
				}
			}
		}
		param.Var = &ir.Var{Name: name, Type: param.Type, Pos: param.Pos}
		out = append(out, param)
	}
	return out
}

func (c *checker) resolveSignatures() {
	for _, fn := range c.allFuncs {
		var params []*ast.Param
		var result ast.Type
		var name *ast.Ident
		switch d := fn.node.(type) {
		case *ast.FnDecl:
			params, result, name = d.Params, d.Result, d.Name
		case *ast.ExternDecl:
			params, result, name = d.Params, d.Result, d.Name
		}
		if fn.feature != nil {
			if top, ok := c.funcs[name.Name]; ok {
				c.errorAt(fn.file, diag.V207, name, "fn "+name.Name, top.where(), fn.where())
			}
		}
		fn.ir.Params = c.params(fn.file, params, ir.InBody, "fn "+name.Name, map[string]bool{})
		fn.ir.Result = c.resolveType(fn.file, result)
	}
}

func (c *checker) resolveRoutes() {
	c.routes = append(c.routes, &route{
		ir:   &ir.Route{Method: "GET", Path: "/healthz", Result: ir.Basic(ir.JSON), Status: "ok", Origin: ir.FromBuiltin},
		desc: "by the built-in health check",
	})
	c.authRoutes()
	for _, ft := range c.featList {
		tests := map[string]bool{}
		for _, item := range ft.ast.Items {
			switch d := item.(type) {
			case *ast.CrudDecl:
				c.expandCrud(ft, d)
			case *ast.RouteDecl:
				c.declareRoute(ft, d)
			case *ast.TestDecl:
				name, ok := plainString(d.Name)
				if !ok {
					c.errorAt(ft.file, diag.V301, d.Name, "a test name is a plain string, without {...}")
				}
				if tests[name] {
					c.errorAt(ft.file, diag.V204, d.Name, "test "+quote(name), "feature "+ft.ir.Name)
					continue
				}
				tests[name] = true
				t := &ir.Test{Name: name, Feature: ft.ir.Name, Pos: pos(ft.file, d.Pos())}
				ft.ir.Tests = append(ft.ir.Tests, t)
				c.tests = append(c.tests, &test{ir: t, decl: decl{ft.file, d}, ast: d, feat: ft})
			}
		}
	}
	c.checkDuplicateRoutes()
	// Show routes as declared: auth first, then features, then built-ins.
	for _, r := range c.routes[1:] {
		c.prog.Routes = append(c.prog.Routes, r.ir)
	}
	c.prog.Routes = append(c.prog.Routes, c.routes[0].ir)
}

func (c *checker) authRoutes() {
	if c.auth == nil {
		return
	}
	m := c.auth.ir
	at := *c.authDecl
	p := pos(at.file, at.node.Pos())
	body := func(name string, t *ir.Type) *ir.Param {
		return &ir.Param{Name: name, Type: t, In: ir.InBody, Pos: p}
	}
	register := []*ir.Param{body("email", ir.Basic(ir.Email)), body("password", ir.Basic(ir.Text))}
	for _, f := range m.Fields {
		if f.Required() && f.Name != "email" {
			register = append(register, body(f.Name, f.Type))
		}
	}
	desc := "by auth " + m.Name + " at " + at.where()
	for _, r := range []*ir.Route{
		{Method: "POST", Path: "/auth/register", Params: register, Result: ir.ModelOf(m), Status: "created", Op: "register"},
		{Method: "POST", Path: "/auth/login", Params: []*ir.Param{body("email", ir.Basic(ir.Email)), body("password", ir.Basic(ir.Text))}, Result: ir.ModelOf(m), Status: "ok", Op: "login"},
		{Method: "POST", Path: "/auth/logout", Result: ir.Basic(ir.Empty), Status: "empty", Op: "logout"},
		{Method: "GET", Path: "/auth/me", Result: ir.ModelOf(m), Status: "ok", Login: true, Op: "me"},
	} {
		r.Origin, r.Model, r.Pos = ir.FromAuth, m, p
		c.routes = append(c.routes, &route{ir: r, decl: at, desc: desc})
	}
}

// pathPattern returns the net/http pattern of a declared path.
func pathPattern(p *ast.Path) string {
	if len(p.Segments) == 0 {
		return "/"
	}
	var b strings.Builder
	for _, s := range p.Segments {
		b.WriteByte('/')
		if id, ok := s.X.(*ast.Ident); ok {
			b.WriteString("{" + id.Name + "}")
		} else {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// pathParamType types a path parameter by its name.
func pathParamType(name string) *ir.Type {
	if name == "id" || strings.HasSuffix(name, "_id") {
		return ir.Basic(ir.ID)
	}
	return ir.Basic(ir.Text)
}

func (c *checker) declareRoute(ft *feature, d *ast.RouteDecl) {
	f := ft.file
	r := &ir.Route{
		Method:  strings.ToUpper(d.Method.String()),
		Path:    pathPattern(d.Path),
		Origin:  ir.FromRoute,
		Feature: ft.ir.Name,
		Pos:     pos(f, d.Pos()),
	}
	owner := "route " + r.Method + " " + r.Path
	taken := map[string]bool{}
	for _, s := range d.Path.Segments {
		id, ok := s.X.(*ast.Ident)
		if !ok {
			continue
		}
		if taken[id.Name] {
			c.errorAt(f, diag.V204, id, "path parameter "+quote(id.Name), owner)
			continue
		}
		taken[id.Name] = true
		t := pathParamType(id.Name)
		p := &ir.Param{Name: id.Name, Type: t, In: ir.InPath, Pos: pos(f, id.Pos())}
		p.Var = &ir.Var{Name: id.Name, Type: t, Pos: p.Pos}
		r.Params = append(r.Params, p)
	}
	in := ir.InBody
	if d.Method == token.Get || d.Method == token.Delete {
		in = ir.InQuery
	}
	r.Params = append(r.Params, c.params(f, d.Params, in, owner, taken)...)
	c.routes = append(c.routes, &route{ir: r, decl: decl{f, d}, ast: d, desc: "at " + decl{f, d}.where(), feat: ft})
}

var crudOrder = []string{"list", "get", "create", "update", "delete"}

func (c *checker) expandCrud(ft *feature, d *ast.CrudDecl) {
	f := ft.file
	m, ok := c.models[d.Model.Name]
	if !ok {
		c.unknownModel(f, diag.V208, d.Model)
		return
	}
	key := m.ir.Field("id")
	segs := d.Path.Segments
	if n := len(segs); n > 0 && segs[n-1].X != nil {
		id := segs[n-1].X.(*ast.Ident)
		field := m.ir.Field(id.Name)
		switch {
		case field == nil:
			e := c.diagAt(f, diag.V403, segs[n-1], m.ir.Name, "{"+id.Name+"}", m.ir.Name+" has no field "+quote(id.Name))
			if s, ok := diag.Suggest(id.Name, fieldNames(m.ir)); ok {
				e = e.WithFix("Use {"+s+"}", diag.Replace(f, id.Pos(), id.End(), s))
			}
			c.add(e)
			return
		case !field.Unique:
			c.errorAt(f, diag.V404, segs[n-1], m.ir.Name, quote(id.Name))
			return
		}
		key, segs = field, segs[:n-1]
	}
	var base strings.Builder
	for _, s := range segs {
		if s.X != nil {
			c.errorAt(f, diag.V403, s, m.ir.Name, f.Text(s.Pos(), s.End()), "it is not the last segment; crud fills only the lookup key at the end")
			return
		}
		base.WriteString("/" + s.Text)
	}
	basePath := base.String()
	if basePath == "" {
		basePath = "/"
	}
	itemPath := strings.TrimSuffix(basePath, "/") + "/{" + key.Name + "}"

	var owner *ir.Field
	if d.Owner != nil {
		field := m.ir.Field(d.Owner.Name)
		switch {
		case field == nil:
			c.unknownField(f, m.ir, d.Owner)
			return
		case c.auth == nil:
			c.errorAt(f, diag.V406, d.Owner, m.ir.Name, quote(field.Name), "the app has no auth declaration")
			return
		case field.Ref != c.auth.ir:
			c.errorAt(f, diag.V406, d.Owner, m.ir.Name, quote(field.Name), "it must be a Ref to the auth model "+c.auth.ir.Name+", not "+describeType(field))
			return
		}
		owner = field
	}
	ops := crudOrder
	if len(d.Only) > 0 {
		ops = nil
		for _, op := range d.Only {
			if slices.Contains(ops, op.Name) {
				c.errorAt(f, diag.V204, op, "operation "+quote(op.Name), "crud "+m.ir.Name)
				continue
			}
			ops = append(ops, op.Name)
		}
	}
	at := decl{f, d}
	p := pos(f, d.Pos())
	keyParam := func() *ir.Param { return &ir.Param{Name: key.Name, Type: key.Type, In: ir.InPath, Pos: p} }
	fieldParams := func(allOptional bool) []*ir.Param {
		var ps []*ir.Param
		for _, fl := range m.ir.Fields {
			if fl.Implicit || fl == owner {
				continue
			}
			t := fl.Type
			if allOptional || !fl.Required() {
				t = ir.OptionalOf(t)
			}
			p := &ir.Param{Name: fl.Name, Type: t, In: ir.InBody, Pos: fl.Pos}
			if !allOptional {
				// An update changes only the fields it is given, so
				// defaults apply to create alone.
				p.Default = fl.Default
			}
			ps = append(ps, p)
		}
		return ps
	}
	record := ir.ModelOf(m.ir)
	for _, op := range crudOrder {
		if !slices.Contains(ops, op) {
			continue
		}
		r := &ir.Route{Origin: ir.FromCrud, Feature: ft.ir.Name, Model: m.ir, Op: op, Key: key, Owner: owner, Private: d.Private, Pos: p}
		switch op {
		case "list":
			r.Method, r.Path, r.Result, r.Status, r.Login = "GET", basePath, ir.ListOf(record), "ok", d.Private
			r.Params = []*ir.Param{
				{Name: "limit", Type: ir.Basic(ir.Int), In: ir.InQuery, Default: intConst(50, p), Pos: p},
				{Name: "offset", Type: ir.Basic(ir.Int), In: ir.InQuery, Default: intConst(0, p), Pos: p},
			}
		case "get":
			r.Method, r.Path, r.Result, r.Status, r.Login = "GET", itemPath, record, "ok", d.Private
			r.Params = []*ir.Param{keyParam()}
		case "create":
			r.Method, r.Path, r.Result, r.Status, r.Login = "POST", basePath, record, "created", owner != nil
			r.Params = fieldParams(false)
		case "update":
			r.Method, r.Path, r.Result, r.Status, r.Login = "PATCH", itemPath, record, "ok", owner != nil
			r.Params = append([]*ir.Param{keyParam()}, fieldParams(true)...)
		case "delete":
			r.Method, r.Path, r.Result, r.Status, r.Login = "DELETE", itemPath, ir.Basic(ir.Empty), "empty", owner != nil
			r.Params = []*ir.Param{keyParam()}
		}
		c.routes = append(c.routes, &route{ir: r, decl: at, desc: "by crud " + m.ir.Name + " at " + at.where(), feat: ft})
	}
}

func intConst(n int64, p ir.Pos) *ir.Const {
	k := &ir.Const{Value: n}
	k.T, k.P = ir.Basic(ir.Int), p
	return k
}

// checkDuplicateRoutes reports V401 for two routes with the same method
// and path. Parameter names do not count: /books/{id} and /books/{slug}
// are the same path to the router.
func (c *checker) checkDuplicateRoutes() {
	seen := map[string]*route{}
	for _, r := range c.routes {
		key := r.ir.Method + " " + normalizePath(r.ir.Path)
		prev, ok := seen[key]
		if !ok {
			seen[key] = r
			continue
		}
		if r.file != nil {
			c.errorAt(r.file, diag.V401, r.node, r.ir.Method+" "+r.ir.Path, prev.desc, r.desc)
		}
	}
}

func normalizePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if strings.HasPrefix(s, "{") {
			segs[i] = "{}"
		}
	}
	return strings.Join(segs, "/")
}

// describeType names the declared type of a field: Ref fields by target.
func describeType(f *ir.Field) string {
	if f.Ref != nil {
		return "Ref " + f.Ref.Name
	}
	return f.Type.String()
}
