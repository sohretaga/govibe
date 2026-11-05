package sema

import (
	"slices"
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/token"
)

var (
	modelMethods  = []string{"all", "where", "find", "add"}
	queryMethods  = []string{"order", "newest", "limit", "offset", "first", "count"}
	recordMethods = []string{"update", "delete"}
	textMethods   = []string{"lower", "upper", "trim", "starts_with", "ends_with"}

	methodAliases = map[string]string{
		"filter": "where", "select": "where", "get": "find", "create": "add", "insert": "add", "new": "add", "list": "all",
		"sort": "order", "order_by": "order", "sort_by": "order", "take": "limit", "skip": "offset",
		"len": "count", "length": "count", "size": "count", "one": "first",
		"save": "update", "remove": "delete", "destroy": "delete",
		"to_lower": "lower", "to_upper": "upper", "strip": "trim", "startswith": "starts_with", "endswith": "ends_with",
	}
)

// unknownMethod reports V303 with the methods that exist and a fix for a
// likely intended one.
func (e *env) unknownMethod(sel *ast.Ident, owner string, methods []string) {
	d := e.diag(diag.V303, sel, owner, quote(sel.Name), "; it has "+strings.Join(methods, ", "))
	alias, ok := methodAliases[sel.Name]
	if !ok || !slices.Contains(methods, alias) {
		alias, ok = diag.Suggest(sel.Name, methods)
	}
	if ok {
		d = d.WithFix("Use "+quote(alias), diag.Replace(e.f, sel.Pos(), sel.End(), alias))
	}
	e.c.add(d)
}

func (e *env) method(x *ast.CallExpr, sel *ast.SelectorExpr) ir.Expr {
	name := sel.Sel.Name
	if id, ok := sel.X.(*ast.Ident); ok && isUpper(id.Name) {
		m, ok := e.c.models[id.Name]
		if !ok {
			e.c.unknownModel(e.f, diag.V208, id)
			return e.invalid(x)
		}
		return e.modelMethod(x, sel, m.ir)
	}
	recv := e.expr(sel.X, nil)
	t := recv.Type()
	switch {
	case t.Kind == ir.Invalid:
		return e.invalid(x)
	case t.Kind == ir.Query:
		return e.queryMethod(x, sel, recv.(*ir.QueryExpr))
	case t.Kind == ir.Record:
		return e.recordMethod(x, sel, recv)
	case t.Kind == ir.Optional:
		e.errorf(diag.V301, sel.X, e.f.Text(sel.X.Pos(), sel.X.End())+" is optional ("+t.String()+"); test it with x != none before calling "+name)
		return e.invalid(x)
	case t.IsText():
		return e.textMethod(x, sel, recv)
	case t.Kind == ir.List:
		hint := ""
		if name == "count" || name == "len" || name == "length" || name == "size" {
			hint = "; use len(x) for the number of elements"
		}
		e.errorf(diag.V303, sel.Sel, t.String(), quote(name), hint)
		return e.invalid(x)
	}
	e.errorf(diag.V303, sel.Sel, t.String(), quote(name), "")
	return e.invalid(x)
}

func (e *env) noArgs(x *ast.CallExpr, what string) bool {
	if len(x.Args) > 0 {
		e.errorf(diag.V302, x, what, "it takes no arguments")
		return false
	}
	return true
}

func (e *env) modelMethod(x *ast.CallExpr, sel *ast.SelectorExpr, m *ir.Model) ir.Expr {
	name := sel.Sel.Name
	what := m.Name + "." + name
	if !slices.Contains(modelMethods, name) {
		e.unknownMethod(sel.Sel, m.Name, modelMethods)
		return e.invalid(x)
	}
	e.effect(x, "run queries")
	p := e.pos(x)
	switch name {
	case "all":
		e.noArgs(x, what)
		q := &ir.QueryExpr{Model: m}
		q.T, q.P = ir.QueryOf(m), p
		return q
	case "where":
		q := &ir.QueryExpr{Model: m}
		q.T, q.P = ir.QueryOf(m), p
		if len(x.Args) == 0 {
			e.errorf(diag.V302, x, what, "it takes at least one condition; use "+m.Name+".all() for every record")
		}
		saved := e.where
		e.where = m
		for _, a := range x.Args {
			if a.Name != nil {
				e.errorf(diag.V302, a, what, "conditions have no names; write "+a.Name.Name+" == ...")
				continue
			}
			q.Where = append(q.Where, e.cond(a.Value, true))
		}
		e.where = saved
		return q
	case "find":
		return e.find(x, m)
	}
	out := &ir.Add{Model: m, Values: e.fieldValues(x, m, what, true)}
	out.T, out.P = ir.ModelOf(m), p
	return out
}

func (e *env) find(x *ast.CallExpr, m *ir.Model) ir.Expr {
	what := m.Name + ".find"
	if len(x.Args) != 1 {
		e.errorf(diag.V302, x, what, "it takes an id, as in find(id), or one unique field, as in find(slug: s)")
		return e.invalid(x)
	}
	a := x.Args[0]
	field := m.Field("id")
	if a.Name != nil {
		field = m.Field(a.Name.Name)
		switch {
		case field == nil:
			e.c.unknownField(e.f, m, a.Name)
			return e.invalid(x)
		case !field.Unique:
			d := e.diag(diag.V206, a.Name, m.Name, quote(field.Name))
			fixed := m.Name + ".where(" + field.Name + " == " + e.f.Text(a.Value.Pos(), a.Value.End()) + ").first()"
			e.c.add(d.WithFix("Use "+fixed, diag.Replace(e.f, x.Pos(), x.End(), fixed)))
			return e.invalid(x)
		}
	}
	v := e.expr(a.Value, field.Type)
	e.assign(v, field.Type, a.Value, "the "+field.Name+" given to "+what)
	out := &ir.Find{Model: m, Field: field, Value: v}
	out.T, out.P = ir.ModelOf(m), e.pos(x)
	return out
}

// fieldValues checks the named arguments of add or update. A create
// must give every required field.
func (e *env) fieldValues(x *ast.CallExpr, m *ir.Model, what string, create bool) []ir.FieldValue {
	var out []ir.FieldValue
	seen := map[*ir.Field]bool{}
	for _, a := range x.Args {
		if a.Name == nil {
			e.errorf(diag.V302, a, what, "it takes named fields, as in "+what+"(title: \"Dune\")")
			continue
		}
		f := m.Field(a.Name.Name)
		switch {
		case f == nil:
			e.c.unknownField(e.f, m, a.Name)
			e.expr(a.Value, nil)
			continue
		case f.Implicit:
			e.errorf(diag.V302, a.Name, what, quote(f.Name)+" is set automatically")
			continue
		case seen[f]:
			e.errorf(diag.V204, a.Name, "argument "+quote(f.Name), "the call to "+what)
			continue
		}
		seen[f] = true
		v := e.expr(a.Value, f.Type)
		e.assign(v, f.Type, a.Value, "field "+f.Name+" of "+m.Name)
		out = append(out, ir.FieldValue{Field: f, Value: v})
	}
	if create {
		var missing []string
		for _, f := range m.Fields {
			if f.Required() && !seen[f] {
				missing = append(missing, quote(f.Name))
			}
		}
		if len(missing) > 0 {
			e.errorf(diag.V302, x, what, "missing required "+plural2(len(missing), "field", "fields")+" "+strings.Join(missing, ", "))
		}
	} else if len(x.Args) == 0 {
		e.errorf(diag.V302, x, what, "it takes at least one field to change")
	}
	return out
}

func plural2(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (e *env) queryMethod(x *ast.CallExpr, sel *ast.SelectorExpr, q *ir.QueryExpr) ir.Expr {
	name := sel.Sel.Name
	what := "Query " + q.Model.Name
	if name == "where" {
		e.errorf(diag.V303, sel.Sel, what, "'where'", "; put every condition in one where, as in "+q.Model.Name+".where(a, b)")
		return e.invalid(x)
	}
	if !slices.Contains(queryMethods, name) {
		e.unknownMethod(sel.Sel, what, queryMethods)
		return e.invalid(x)
	}
	out := *q
	out.P = e.pos(x)
	out.Where = slices.Clip(q.Where)
	out.Orders = slices.Clip(q.Orders)
	call := name + "()"
	switch name {
	case "order":
		if order, ok := e.order(x, q.Model); ok {
			out.Orders = append(out.Orders, order)
		}
	case "newest":
		e.noArgs(x, call)
		out.Orders = append(out.Orders, ir.Order{Field: q.Model.Field("created_at"), Desc: true})
	case "limit", "offset":
		if len(x.Args) != 1 || x.Args[0].Name != nil {
			e.errorf(diag.V302, x, call, "it takes one Int")
			break
		}
		v := e.expr(x.Args[0].Value, ir.Basic(ir.Int))
		e.assign(v, ir.Basic(ir.Int), x.Args[0].Value, "the argument of "+name)
		if name == "limit" {
			out.Limit = v
		} else {
			out.Offset = v
		}
	case "first":
		e.noArgs(x, call)
		out.Result, out.T = ir.First, ir.OptionalOf(ir.ModelOf(q.Model))
	case "count":
		e.noArgs(x, call)
		out.Result, out.T = ir.Count, ir.Basic(ir.Int)
	}
	return &out
}

// order checks order(field) and order(-field).
func (e *env) order(x *ast.CallExpr, m *ir.Model) (ir.Order, bool) {
	if len(x.Args) != 1 || x.Args[0].Name != nil {
		e.errorf(diag.V302, x, "order", "it takes one field, as in order(title) or order(-created_at)")
		return ir.Order{}, false
	}
	arg := x.Args[0].Value
	desc := false
	if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.Minus {
		arg, desc = u.X, true
	}
	id, ok := arg.(*ast.Ident)
	if !ok {
		e.errorf(diag.V308, arg, "order takes a field of "+m.Name+", as in order(title) or order(-created_at)")
		return ir.Order{}, false
	}
	f := m.Field(id.Name)
	if f == nil {
		e.c.unknownField(e.f, m, id)
		return ir.Order{}, false
	}
	return ir.Order{Field: f, Desc: desc}, true
}

func (e *env) recordMethod(x *ast.CallExpr, sel *ast.SelectorExpr, recv ir.Expr) ir.Expr {
	m := recv.Type().Model
	name := sel.Sel.Name
	if !slices.Contains(recordMethods, name) {
		if m.Field(name) != nil {
			e.errorf(diag.V303, sel.Sel, m.Name, quote(name), "; "+name+" is a field, read it without ()")
		} else {
			e.unknownMethod(sel.Sel, m.Name+" record", recordMethods)
		}
		return e.invalid(x)
	}
	e.effect(x, "run queries")
	if name == "delete" {
		e.noArgs(x, "delete()")
		out := &ir.Delete{X: recv}
		out.T, out.P = ir.Basic(ir.Empty), e.pos(x)
		return out
	}
	out := &ir.Update{X: recv, Values: e.fieldValues(x, m, "update", false)}
	out.T, out.P = ir.ModelOf(m), e.pos(x)
	return out
}

func (e *env) textMethod(x *ast.CallExpr, sel *ast.SelectorExpr, recv ir.Expr) ir.Expr {
	name := sel.Sel.Name
	if !slices.Contains(textMethods, name) {
		e.unknownMethod(sel.Sel, "Text", textMethods)
		return e.invalid(x)
	}
	out := &ir.Builtin{Name: name, Args: []ir.Expr{recv}}
	out.P = e.pos(x)
	switch name {
	case "starts_with", "ends_with":
		args, ok := e.positional(x, name, 1)
		if !ok {
			return e.invalid(x)
		}
		e.assign(args[0], ir.Basic(ir.Text), x.Args[0].Value, "the argument of "+name)
		out.Args, out.T = append(out.Args, args[0]), ir.Basic(ir.Bool)
	default:
		e.noArgs(x, name+"()")
		out.T = ir.Basic(ir.Text)
	}
	return out
}
