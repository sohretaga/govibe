package sema

import (
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/token"
)

func (c *checker) resolveModels() {
	for _, m := range c.order {
		m.ir.Fields = []*ir.Field{
			{Name: "id", Column: "id", Type: ir.Basic(ir.ID), Unique: true, Implicit: true, Pos: m.ir.Pos},
			{Name: "created_at", Column: "created_at", Type: ir.Basic(ir.Time), Implicit: true, Pos: m.ir.Pos},
			{Name: "updated_at", Column: "updated_at", Type: ir.Basic(ir.Time), Implicit: true, Pos: m.ir.Pos},
		}
		for _, fd := range m.ast.Fields {
			if f := c.resolveField(m, fd); f != nil {
				m.ir.Fields = append(m.ir.Fields, f)
			}
		}
		c.resolveSlugs(m)
	}
}

func (c *checker) resolveField(m *model, fd *ast.Field) *ir.Field {
	f := m.file
	name := fd.Name.Name
	if prev := m.ir.Field(name); prev != nil {
		owner := "model " + m.ir.Name
		if prev.Implicit {
			owner += ", which has id, created_at and updated_at already"
		}
		c.errorAt(f, diag.V204, fd.Name, "field "+quote(name), owner)
		return nil
	}
	field := &ir.Field{Name: name, Column: name, Pos: pos(f, fd.Pos())}
	optional := false
	t := fd.Type
	if o, ok := t.(*ast.OptionalType); ok {
		optional, t = true, o.Elem
	}
	switch t := t.(type) {
	case *ast.NamedType:
		field.Type = ir.Basic(baseTypes[t.Name.Name])
	case *ast.RefType:
		target, ok := c.models[t.Model.Name]
		if !ok {
			c.unknownModel(f, diag.V501, t.Model)
			field.Type = ir.Basic(ir.Invalid)
			break
		}
		field.Type, field.Ref, field.Column = ir.Basic(ir.ID), target.ir, name+"_id"
	case *ast.SlugType:
		field.Type, field.Unique = ir.Basic(ir.Slug), true
	}
	if field.Type == nil {
		field.Type = ir.Basic(ir.Invalid)
	}
	base := field.Type
	if optional {
		field.Type = ir.OptionalOf(field.Type)
	}
	desc := base.String() + " field " + name
	if field.Ref != nil {
		desc = "Ref field " + name
	}
	seen := map[token.Kind]bool{}
	for _, mod := range fd.Mods {
		if seen[mod.Kind] {
			c.errorAt(f, diag.V204, mod, "modifier "+mod.Kind.String(), "field "+name)
			continue
		}
		seen[mod.Kind] = true
		if mod.Kind == token.Unique {
			field.Unique = true
			continue
		}
		bounded := field.Ref == nil && (base.Kind == ir.Text || base.IsNumeric())
		if !bounded {
			c.errorAt(f, diag.V502, mod, mod.Kind.String(), desc)
			continue
		}
		want, what := base, mod.Kind.String()+" of field "+name
		if base.Kind == ir.Text {
			want, what = ir.Basic(ir.Int), mod.Kind.String()+" of Text field "+name+", a length,"
		}
		bound := c.constant(f, mod.Value, want, what)
		if mod.Kind == token.Min {
			field.Min = bound
		} else {
			field.Max = bound
		}
	}
	if fd.Default != nil {
		if base.Kind == ir.Slug {
			c.errorAt(f, diag.V502, fd.Default, "a default", desc+": a slug is generated from its source")
		} else {
			field.Default = c.constant(f, fd.Default, field.Type, "the default of field "+name)
		}
	}
	return field
}

// resolveSlugs links each Slug field to its source once all fields are
// known.
func (c *checker) resolveSlugs(m *model) {
	for _, fd := range m.ast.Fields {
		t := fd.Type
		if o, ok := t.(*ast.OptionalType); ok {
			t = o.Elem
		}
		st, ok := t.(*ast.SlugType)
		field := m.ir.Field(fd.Name.Name)
		if !ok || field == nil || field.Source != nil {
			continue
		}
		src := m.ir.Field(st.Source.Name)
		switch {
		case src == nil:
			c.unknownField(m.file, m.ir, st.Source)
		case src.Ref != nil || src.Type.Kind != ir.Text && !(src.Type.Kind == ir.Optional && src.Type.Elem.Kind == ir.Text):
			c.errorAt(m.file, diag.V505, st.Source, st.Source.Name, m.ir.Name, st.Source.Name+" is "+src.Type.String())
		default:
			field.Source = src
		}
	}
}

// unknownField reports V201 for a name that is not a field of m.
func (c *checker) unknownField(f *token.File, m *ir.Model, id *ast.Ident) {
	d := c.diagAt(f, diag.V201, id, m.Name, quote(id.Name))
	if s, ok := diag.Suggest(id.Name, fieldNames(m)); ok {
		d = d.WithFix("Did you mean "+quote(s)+"?", diag.Replace(f, id.Pos(), id.End(), s))
	}
	c.add(d)
}

func fieldNames(m *ir.Model) []string {
	names := make([]string, len(m.Fields))
	for i, f := range m.Fields {
		names[i] = f.Name
	}
	return names
}

func (c *checker) resolveAuth() {
	if c.authDecl == nil {
		return
	}
	f := c.authDecl.file
	d := c.authDecl.node.(*ast.AuthDecl)
	m, ok := c.models[d.Model.Name]
	if !ok {
		c.unknownModel(f, diag.V208, d.Model)
		return
	}
	c.auth = m
	c.prog.Auth = m.ir
	email := m.ir.Field("email")
	var fd *ast.Field
	for _, x := range m.ast.Fields {
		if x.Name.Name == "email" {
			fd = x
		}
	}
	name := m.ast.Name
	switch {
	case email == nil || fd == nil:
		d := c.diagAt(m.file, diag.V503, name, m.ir.Name, "the field is missing")
		c.add(d.WithFix("Add the email field", diag.Insert(m.file, m.ast.Lbrace+1, "\n  email: Email unique")))
	case email.Type.Kind != ir.Email:
		d := c.diagAt(m.file, diag.V503, fd.Type, m.ir.Name, "email is "+email.Type.String())
		c.add(d.WithFix("Use Email", diag.Replace(m.file, fd.Type.Pos(), fd.Type.End(), "Email")))
	case !email.Unique:
		d := c.diagAt(m.file, diag.V503, fd.Name, m.ir.Name, "email is not unique")
		c.add(d.WithFix("Add unique", diag.Insert(m.file, fd.Type.End(), " unique")))
	}
}

// constant converts a literal to a constant of type want, or of its own
// type when want is nil. It reports a mismatch and returns nil.
func (c *checker) constant(f *token.File, x ast.Expr, want *ir.Type, what string) *ir.Const {
	k := &ir.Const{}
	k.P = pos(f, x.Pos())
	target := want
	if want != nil && want.Kind == ir.Optional {
		target = want.Elem
	}
	natural := literalType(x)
	mismatch := func() *ir.Const {
		c.errorAt(f, diag.V301, x, what+" must be "+want.String()+", not "+natural)
		return nil
	}
	switch x := x.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.Int:
			n, _ := strconv.ParseInt(strings.ReplaceAll(x.Value, "_", ""), 10, 64)
			switch {
			case target == nil || target.Kind == ir.Int:
				k.T, k.Value = ir.Basic(ir.Int), n
			case target.Kind == ir.Float:
				k.T, k.Value = ir.Basic(ir.Float), float64(n)
			case target.Kind == ir.Money:
				k.T, k.Value = ir.Basic(ir.Money), n*100
			default:
				return mismatch()
			}
		case token.Float:
			switch {
			case target == nil || target.Kind == ir.Float:
				v, _ := strconv.ParseFloat(strings.ReplaceAll(x.Value, "_", ""), 64)
				k.T, k.Value = ir.Basic(ir.Float), v
			case target.Kind == ir.Money:
				cents, ok := moneyLiteral(x.Value)
				if !ok {
					c.errorAt(f, diag.V304, x, x.Value)
					return nil
				}
				k.T, k.Value = ir.Basic(ir.Money), cents
			default:
				return mismatch()
			}
		case token.True, token.False:
			if target != nil && target.Kind != ir.Bool {
				return mismatch()
			}
			k.T, k.Value = ir.Basic(ir.Bool), x.Kind == token.True
		case token.None:
			if want != nil && want.Kind != ir.Optional {
				c.errorAt(f, diag.V301, x, what+" cannot be none: "+want.String()+" is not optional")
				return nil
			}
			k.T = ir.Basic(ir.None)
			if want != nil {
				k.T = want
			}
		}
	case *ast.StringLit:
		s, ok := plainString(x)
		if !ok {
			c.errorAt(f, diag.V301, x, what+" must be a plain string, without {...}")
			return nil
		}
		if target != nil && !target.IsText() {
			return mismatch()
		}
		k.T, k.Value = ir.Basic(ir.Text), s
		if target != nil {
			k.T = target
		}
	default:
		return nil
	}
	return k
}

func literalType(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.BasicLit:
		switch x.Kind {
		case token.Int:
			return "Int"
		case token.Float:
			return "Float"
		case token.True, token.False:
			return "Bool"
		case token.None:
			return "none"
		}
	case *ast.StringLit:
		return "Text"
	}
	return "a value"
}

// moneyLiteral converts a decimal literal such as 7.50 to minor units. It
// fails when the literal has more than two decimal places.
func moneyLiteral(lit string) (int64, bool) {
	lit = strings.ReplaceAll(lit, "_", "")
	whole, frac, _ := strings.Cut(lit, ".")
	if len(frac) > 2 {
		return 0, false
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, _ := strconv.ParseInt(whole, 10, 64)
	fr, _ := strconv.ParseInt(frac, 10, 64)
	return w*100 + fr, true
}
