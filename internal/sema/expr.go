package sema

import (
	"slices"
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/token"
)

var statusNames = []string{"ok", "created", "empty", "bad_request", "login_required", "forbidden", "not_found", "conflict", "invalid", "rate_limited", "internal"}

var httpStatus = map[string]string{
	"200": "ok", "201": "created", "204": "empty", "400": "bad_request", "401": "login_required", "403": "forbidden",
	"404": "not_found", "409": "conflict", "422": "invalid", "429": "rate_limited", "500": "internal",
}

func (e *env) invalid(n ast.Node) ir.Expr {
	k := &ir.Const{}
	k.T, k.P = ir.Basic(ir.Invalid), e.pos(n)
	return k
}

// expr checks x. Literals take the type want when it is a numeric type
// they convert to; want is not otherwise enforced.
func (e *env) expr(x ast.Expr, want *ir.Type) ir.Expr {
	allow := e.allowColumn
	e.allowColumn = false
	switch x := x.(type) {
	case *ast.Ident:
		return e.ident(x, allow)
	case *ast.BasicLit:
		return e.literal(x, want)
	case *ast.StringLit:
		return e.str(x)
	case *ast.MeExpr:
		return e.me(x)
	case *ast.ParenExpr:
		e.allowColumn = allow
		return e.expr(x.X, want)
	case *ast.ListLit:
		return e.list(x, want)
	case *ast.UnaryExpr:
		return e.unary(x, want, allow)
	case *ast.BinaryExpr:
		return e.binary(x, want, allow)
	case *ast.SelectorExpr:
		return e.selector(x)
	case *ast.CallExpr:
		return e.call(x)
	case *ast.HTTPExpr:
		return e.request(x)
	}
	return e.invalid(x)
}

// exprAt checks x at a position where a field of the where model may
// appear when allow is set.
func (e *env) exprAt(x ast.Expr, want *ir.Type, allow bool) ir.Expr {
	e.allowColumn = allow
	return e.expr(x, want)
}

// cond checks a condition, which must be Bool.
func (e *env) cond(x ast.Expr, allow bool) ir.Expr {
	v := e.exprAt(x, ir.Basic(ir.Bool), allow)
	switch t := v.Type(); t.Kind {
	case ir.Bool, ir.Invalid, ir.JSON:
	default:
		msg := "a condition must be Bool, not " + t.String()
		if t.Kind == ir.Optional && t.Elem.Kind == ir.Bool {
			msg += "; it is optional, so test it with x != none first"
		}
		e.errorf(diag.V301, x, msg)
		return e.invalid(x)
	}
	return v
}

// assign reports V301 when v cannot fill a place of type want.
func (e *env) assign(v ir.Expr, want *ir.Type, n ast.Node, what string) bool {
	got := v.Type()
	if assignable(got, want) {
		return true
	}
	msg := what + " must be " + want.String() + ", not " + got.String()
	switch {
	case got.Kind == ir.Empty:
		msg = what + " must be " + want.String() + ", but this has no value"
	case got.Kind == ir.Optional && assignable(got.Elem, want):
		msg += "; it is optional, so test it with x != none first"
	case got.Kind == ir.ModelName:
		msg = what + " must be " + want.String() + "; " + got.Model.Name + " is a model, not a value"
	}
	e.errorf(diag.V301, n, msg)
	return false
}

func (e *env) ident(x *ast.Ident, allow bool) ir.Expr {
	name := x.Name
	if isUpper(name) {
		m, ok := e.c.models[name]
		if !ok {
			e.c.unknownModel(e.f, diag.V208, x)
			return e.invalid(x)
		}
		k := &ir.Const{}
		k.T, k.P = &ir.Type{Kind: ir.ModelName, Model: m.ir}, e.pos(x)
		return k
	}
	b := e.scope.lookup(name)
	if e.where != nil {
		if f := e.where.Field(name); f != nil {
			if b != nil {
				e.errorf(diag.V205, x, quote(name), e.where.Name, "local name; rename the local")
				return e.invalid(x)
			}
			if !allow {
				e.errorf(diag.V308, x, quote(name)+" is a field of "+e.where.Name+"; a field can only be compared, tested with contains or used alone as a Bool")
				return e.invalid(x)
			}
			col := &ir.Column{Field: f}
			col.T, col.P = f.Type, e.pos(x)
			return col
		}
	}
	if b != nil {
		ref := &ir.VarRef{Var: b.v}
		ref.T, ref.P = b.t, e.pos(x)
		return ref
	}
	if e.lookupFunc(name) != nil || slices.Contains(builtins, name) {
		e.errorf(diag.V301, x, name+" is a function; call it, as in "+name+"(...)")
		return e.invalid(x)
	}
	if slices.Contains(statusNames, name) {
		e.errorf(diag.V202, x, quote(name), "; status names are used after fail and compared with a response status, as in res.status == "+name)
		return e.invalid(x)
	}
	if e.where != nil {
		// Inside where, a bare name is first of all a field.
		e.c.unknownField(e.f, e.where, x)
		return e.invalid(x)
	}
	e.unknownName(x)
	return e.invalid(x)
}

func (e *env) unknownName(x *ast.Ident) {
	candidates := e.scope.visible()
	for name := range e.c.funcs {
		candidates = append(candidates, name)
	}
	if e.feat != nil {
		for name := range e.feat.funcs {
			candidates = append(candidates, name)
		}
	}
	if e.where != nil {
		candidates = append(candidates, fieldNames(e.where)...)
	}
	slices.Sort(candidates)
	d := e.diag(diag.V202, x, quote(x.Name), "")
	if s, ok := diag.Suggest(x.Name, candidates); ok {
		d = d.WithFix("Did you mean "+quote(s)+"?", diag.Replace(e.f, x.Pos(), x.End(), s))
	}
	e.c.add(d)
}

// lookupFunc finds a fn of the current feature, then a top-level one.
func (e *env) lookupFunc(name string) *function {
	if e.feat != nil {
		if fn, ok := e.feat.funcs[name]; ok {
			return fn
		}
	}
	return e.c.funcs[name]
}

func isUpper(name string) bool { return name != "" && 'A' <= name[0] && name[0] <= 'Z' }

func (e *env) literal(x *ast.BasicLit, want *ir.Type) ir.Expr {
	target := want
	if target != nil && target.Kind == ir.Optional {
		target = target.Elem
	}
	if target != nil && target.Kind != ir.Float && target.Kind != ir.Money {
		target = nil
	}
	if x.Kind == token.Int && target == nil || x.Kind == token.Float && (target == nil || target.Kind == ir.Float) {
		target = nil
	}
	if x.Kind == token.None {
		// none adapts only to an optional; next to a plain value it stays
		// none, so the comparison reports that the value is never none.
		target = nil
		if want != nil && want.Kind == ir.Optional {
			target = want
		}
	}
	k := e.c.constant(e.f, x, target, "the value")
	if k == nil {
		return e.invalid(x)
	}
	return k
}

func (e *env) str(x *ast.StringLit) ir.Expr {
	var parts []ir.Expr
	plain := true
	for _, p := range x.Parts {
		if p.X == nil {
			k := &ir.Const{Value: p.Text}
			k.T, k.P = ir.Basic(ir.Text), e.pos(x)
			parts = append(parts, k)
			continue
		}
		plain = false
		v := e.expr(p.X, nil)
		switch v.Type().Kind {
		case ir.Empty, ir.ModelName:
			e.errorf(diag.V301, p.X, "this has no value to put in text")
		}
		parts = append(parts, v)
	}
	if plain {
		s, _ := plainString(x)
		k := &ir.Const{Value: s}
		k.T, k.P = ir.Basic(ir.Text), e.pos(x)
		return k
	}
	out := &ir.Interp{Parts: parts}
	out.T, out.P = ir.Basic(ir.Text), e.pos(x)
	return out
}

func (e *env) me(x *ast.MeExpr) ir.Expr {
	switch {
	case e.kind == inFn:
		e.effect(x, "use me")
		return e.invalid(x)
	case e.kind == inTest:
		e.errorf(diag.V202, x, "'me'", "; me exists only in routes, and a test logs in with login_as(user)")
		return e.invalid(x)
	case e.c.auth == nil:
		e.errorf(diag.V202, x, "'me'", "; me needs an auth declaration, such as auth User")
		return e.invalid(x)
	}
	e.usesMe = true
	m := &ir.Me{}
	m.T, m.P = ir.ModelOf(e.c.auth.ir), e.pos(x)
	return m
}

func (e *env) list(x *ast.ListLit, want *ir.Type) ir.Expr {
	var elem *ir.Type
	if want != nil && want.Kind == ir.List {
		elem = want.Elem
	}
	out := &ir.ListExpr{}
	for _, el := range x.Elems {
		v := e.expr(el, elem)
		if elem == nil {
			elem = v.Type().Value()
		} else {
			e.assign(v, elem, el, "a list element")
		}
		out.Elems = append(out.Elems, v)
	}
	if elem == nil {
		elem = ir.Basic(ir.Invalid)
	}
	out.T, out.P = ir.ListOf(elem), e.pos(x)
	return out
}

func (e *env) unary(x *ast.UnaryExpr, want *ir.Type, allow bool) ir.Expr {
	out := &ir.Unary{Op: x.Op.String()}
	out.P = e.pos(x)
	if x.Op == token.Not {
		out.X = e.cond(x.X, allow)
		out.T = ir.Basic(ir.Bool)
		return out
	}
	out.X = e.expr(x.X, want)
	t := out.X.Type()
	if !t.IsNumeric() && t.Kind != ir.Invalid {
		e.errorf(diag.V301, x, "- needs a number, not "+t.String())
		t = ir.Basic(ir.Invalid)
	}
	out.T = t
	return out
}

// isLiteral reports whether x is a number literal, possibly negated or
// in parentheses, whose type comes from the other operand.
func isLiteral(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.BasicLit:
		return x.Kind == token.Int || x.Kind == token.Float
	case *ast.UnaryExpr:
		return x.Op == token.Minus && isLiteral(x.X)
	case *ast.ParenExpr:
		return isLiteral(x.X)
	}
	return false
}

// literalWant returns the type a number literal takes next to an operand
// of type other: the same numeric type, except that money is multiplied
// by an Int.
func literalWant(op token.Kind, other *ir.Type) *ir.Type {
	if other.Kind == ir.Optional {
		other = other.Elem
	}
	switch {
	case other.Kind == ir.Money && (op == token.Star || op == token.Slash || op == token.Percent):
		return ir.Basic(ir.Int)
	case other.IsNumeric():
		return other
	}
	return nil
}

func (e *env) binary(x *ast.BinaryExpr, want *ir.Type, allow bool) ir.Expr {
	if x.Op == token.And || x.Op == token.Or {
		out := &ir.Binary{Op: x.Op.String(), X: e.cond(x.X, allow), Y: e.cond(x.Y, allow)}
		out.T, out.P = ir.Basic(ir.Bool), e.pos(x)
		return out
	}
	operand := allow && x.Op.IsComparison()
	var l, r ir.Expr
	switch {
	case (x.Op == token.Eq || x.Op == token.NotEq) && e.statusCandidate(x.X) && !e.statusCandidate(x.Y):
		r = e.exprAt(x.Y, nil, operand)
		if r.Type().Kind == ir.Status {
			l = e.statusValue(x.X)
		} else {
			l = e.exprAt(x.X, literalWant(x.Op, r.Type()), operand)
		}
	case isLiteral(x.X) && !isLiteral(x.Y):
		r = e.exprAt(x.Y, nil, operand)
		l = e.exprAt(x.X, literalWant(x.Op, r.Type()), operand)
	default:
		var lw *ir.Type
		if isLiteral(x.X) && !x.Op.IsComparison() {
			lw = want
		}
		l = e.exprAt(x.X, lw, operand)
		if (x.Op == token.Eq || x.Op == token.NotEq) && l.Type().Kind == ir.Status && e.statusCandidate(x.Y) {
			r = e.statusValue(x.Y)
		} else {
			r = e.exprAt(x.Y, literalWant(x.Op, l.Type()), operand)
		}
	}
	if x.Op.IsComparison() {
		return e.compare(x, l, r)
	}
	return e.arith(x, l, r)
}

// statusCandidate reports whether x may be a status written by name or by
// number in a comparison with a response status.
func (e *env) statusCandidate(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.Ident:
		return !isUpper(x.Name) && e.scope.lookup(x.Name) == nil
	case *ast.BasicLit:
		return x.Kind == token.Int
	}
	return false
}

func (e *env) statusValue(x ast.Expr) ir.Expr {
	switch x := x.(type) {
	case *ast.Ident:
		if slices.Contains(statusNames, x.Name) {
			s := &ir.StatusName{Name: x.Name}
			s.T, s.P = ir.Basic(ir.Status), e.pos(x)
			return s
		}
		d := e.diag(diag.V202, x, quote(x.Name), "; a status is one of "+strings.Join(statusNames, ", "))
		if s, ok := diag.Suggest(x.Name, statusNames); ok {
			d = d.WithFix("Use "+quote(s), diag.Replace(e.f, x.Pos(), x.End(), s))
		}
		e.c.add(d)
	case *ast.BasicLit:
		d := e.diag(diag.V301, x, "a status is compared by name, not by the number "+x.Value)
		if name, ok := httpStatus[x.Value]; ok {
			d = e.diag(diag.V301, x, "a status is compared by name: write "+name+", not "+x.Value)
			d = d.WithFix("Use "+quote(name), diag.Replace(e.f, x.Pos(), x.End(), name))
		}
		e.c.add(d)
	}
	return e.invalid(x)
}

func isColumn(x ir.Expr) bool {
	_, ok := x.(*ir.Column)
	return ok
}

func (e *env) compare(x *ast.BinaryExpr, l, r ir.Expr) ir.Expr {
	out := &ir.Binary{Op: x.Op.String(), X: l, Y: r}
	out.T, out.P = ir.Basic(ir.Bool), e.pos(x)
	lt, rt := l.Type(), r.Type()
	if e.where != nil {
		// A field compared with an optional value: the condition is
		// dropped when the value is none.
		switch {
		case isColumn(l) && !isColumn(r) && rt.Kind == ir.Optional:
			rt, out.SkipIfNone = rt.Elem, true
		case isColumn(r) && !isColumn(l) && lt.Kind == ir.Optional:
			lt, out.SkipIfNone = lt.Elem, true
		}
		if x.Op == token.Contains && isColumn(r) && !isColumn(l) {
			e.errorf(diag.V308, x, "put the field on the left of contains, as in "+e.f.Text(x.Y.Pos(), x.Y.End())+" contains "+e.f.Text(x.X.Pos(), x.X.End()))
			return out
		}
	}
	ok := true
	switch x.Op {
	case token.Eq, token.NotEq:
		ok = comparable(lt, rt)
	case token.Contains:
		ok = lt.Kind == ir.JSON || lt.Kind == ir.Invalid || rt.Kind == ir.Invalid || lt.IsText() && (rt.IsText() || rt.Kind == ir.JSON)
	default:
		ok = ordered(lt, rt)
	}
	if ok {
		return out
	}
	msg := "cannot compare " + lt.String() + " and " + rt.String() + " with " + x.Op.String()
	switch {
	case lt.Kind == ir.None && rt.Kind != ir.Optional || rt.Kind == ir.None && lt.Kind != ir.Optional:
		msg = "a " + nonNone(lt, rt).String() + " is never none: only an optional value (T?) is compared with none"
	case lt.Kind == ir.Record || rt.Kind == ir.Record:
		msg += "; compare records by id, as in a.id == b.id"
	case x.Op == token.Contains:
		msg = "contains needs Text on both sides, not " + lt.String() + " and " + rt.String()
	case lt.Kind == ir.Optional || rt.Kind == ir.Optional:
		msg += "; an optional value must be tested with x != none first"
	}
	e.errorf(diag.V301, x, msg)
	return out
}

func nonNone(a, b *ir.Type) *ir.Type {
	if a.Kind == ir.None {
		return b
	}
	return a
}

func (e *env) arith(x *ast.BinaryExpr, l, r ir.Expr) ir.Expr {
	out := &ir.Binary{Op: x.Op.String(), X: l, Y: r}
	out.P = e.pos(x)
	lt, rt := l.Type(), r.Type()
	if t := arithmetic(out.Op, lt, rt); t != nil {
		out.T = t
		return out
	}
	out.T = ir.Basic(ir.Invalid)
	op := x.Op.String()
	switch {
	case x.Op == token.Plus && lt.IsText() && rt.IsText():
		d := e.diag(diag.V301, x, "text is joined with interpolation, not +")
		if fixed, ok := e.interpolation(x); ok {
			d = d.WithFix("Use "+fixed, diag.Replace(e.f, x.Pos(), x.End(), fixed))
		}
		e.c.add(d)
	case lt.Kind == ir.Optional || rt.Kind == ir.Optional:
		e.errorf(diag.V301, x, "cannot use "+op+" on "+lt.String()+" and "+rt.String()+"; an optional value must be tested with x != none first")
	case lt.Kind == ir.Money || rt.Kind == ir.Money:
		e.errorf(diag.V301, x, "cannot use "+op+" on "+lt.String()+" and "+rt.String()+": money is added to or subtracted from money, and multiplied by an Int; dividing money belongs in an extern fn")
	case op == "%" && (lt.Kind == ir.Float || rt.Kind == ir.Float):
		e.errorf(diag.V301, x, "% takes Int operands, not "+lt.String()+" and "+rt.String())
	default:
		e.errorf(diag.V301, x, "cannot use "+op+" on "+lt.String()+" and "+rt.String())
	}
	return out
}

// interpolation rewrites a + b on text as one interpolated string.
func (e *env) interpolation(x *ast.BinaryExpr) (string, bool) {
	var b strings.Builder
	b.WriteByte('"')
	for _, side := range []ast.Expr{x.X, x.Y} {
		switch s := side.(type) {
		case *ast.StringLit:
			b.WriteString(e.f.Text(s.Pos()+1, s.End()-1))
		case *ast.BinaryExpr:
			return "", false
		default:
			b.WriteString("{" + e.f.Text(side.Pos(), side.End()) + "}")
		}
	}
	b.WriteByte('"')
	return b.String(), true
}

func (e *env) selector(x *ast.SelectorExpr) ir.Expr {
	name := x.Sel.Name
	if id, ok := x.X.(*ast.Ident); ok && isUpper(id.Name) {
		if _, ok := e.c.models[id.Name]; ok && slices.Contains(modelMethods, name) {
			e.errorf(diag.V301, x, id.Name+"."+name+" is a method; call it: "+id.Name+"."+name+"(...)")
			return e.invalid(x)
		}
	}
	recv := e.expr(x.X, nil)
	t := recv.Type()
	switch t.Kind {
	case ir.Invalid:
		return e.invalid(x)
	case ir.Record:
		f := t.Model.Field(name)
		if f == nil {
			if name == "update" || name == "delete" {
				e.errorf(diag.V301, x, name+" is a method; call it: "+e.f.Text(x.Pos(), x.End())+"(...)")
			} else {
				e.c.unknownField(e.f, t.Model, x.Sel)
			}
			return e.invalid(x)
		}
		out := &ir.FieldRef{X: recv, Field: f}
		out.T, out.P = f.Type, e.pos(x)
		return out
	case ir.Optional:
		e.errorf(diag.V301, x.X, e.f.Text(x.X.Pos(), x.X.End())+" is optional ("+t.String()+"); test it with x != none before reading "+name)
		return e.invalid(x)
	case ir.Response:
		switch name {
		case "status":
			return e.member(x, recv, ir.Status)
		case "body":
			return e.member(x, recv, ir.JSON)
		}
		d := e.diag(diag.V201, x.Sel, "a response", quote(name)+"; it has status and body")
		if s, ok := diag.Suggest(name, []string{"status", "body"}); ok {
			d = d.WithFix("Did you mean "+quote(s)+"?", diag.Replace(e.f, x.Sel.Pos(), x.Sel.End(), s))
		}
		e.c.add(d)
		return e.invalid(x)
	case ir.JSON:
		return e.member(x, recv, ir.JSON)
	case ir.Query, ir.ModelName:
		e.errorf(diag.V301, x, name+" is a method; call it: "+e.f.Text(x.Pos(), x.End())+"(...)")
		return e.invalid(x)
	}
	hint := ""
	if name == "length" || name == "len" || name == "size" || name == "count" {
		hint = "; use len(x)"
	}
	e.errorf(diag.V201, x.Sel, t.String(), quote(name)+hint)
	return e.invalid(x)
}

func (e *env) member(x *ast.SelectorExpr, recv ir.Expr, k ir.Kind) ir.Expr {
	out := &ir.Member{X: recv, Name: x.Sel.Name}
	out.T, out.P = ir.Basic(k), e.pos(x)
	return out
}

func (e *env) call(x *ast.CallExpr) ir.Expr {
	switch fun := x.Fun.(type) {
	case *ast.Ident:
		if isUpper(fun.Name) {
			e.errorf(diag.V301, x, fun.Name+" is a model, not a function; create records with "+fun.Name+".add(...)")
			return e.invalid(x)
		}
		return e.callFunc(x, fun)
	case *ast.SelectorExpr:
		return e.method(x, fun)
	}
	e.errorf(diag.V301, x, "only functions and methods can be called")
	return e.invalid(x)
}

func (e *env) callFunc(x *ast.CallExpr, id *ast.Ident) ir.Expr {
	name := id.Name
	if slices.Contains(builtins, name) && e.lookupFunc(name) == nil {
		return e.builtin(x, name)
	}
	if b := e.scope.lookup(name); b != nil {
		e.errorf(diag.V301, id, name+" is a "+b.t.String()+", not a function")
		return e.invalid(x)
	}
	fn := e.lookupFunc(name)
	if fn == nil {
		e.unknownName(id)
		return e.invalid(x)
	}
	if fn.ir.Extern && e.kind == inFn {
		e.errorf(diag.V311, x, e.fn.ir.Name, name)
	}
	out := &ir.Call{Func: fn.ir, Args: e.args(x, fn.ir.Params, "fn "+name)}
	out.T, out.P = fn.ir.Result, e.pos(x)
	return out
}

// args matches the arguments of a call to params, by position or by
// name, and fills omitted parameters from their defaults.
func (e *env) args(x *ast.CallExpr, params []*ir.Param, what string) []ir.Expr {
	out := make([]ir.Expr, len(params))
	for i, a := range x.Args {
		idx := i
		if a.Name != nil {
			idx = slices.IndexFunc(params, func(p *ir.Param) bool { return p.Name == a.Name.Name })
			if idx < 0 {
				names := make([]string, len(params))
				for j, p := range params {
					names[j] = p.Name
				}
				d := e.diag(diag.V302, a.Name, what, "it has no parameter "+quote(a.Name.Name))
				if s, ok := diag.Suggest(a.Name.Name, names); ok {
					d = d.WithFix("Did you mean "+quote(s)+"?", diag.Replace(e.f, a.Name.Pos(), a.Name.End(), s))
				}
				e.c.add(d)
				e.expr(a.Value, nil)
				continue
			}
			if out[idx] != nil {
				e.errorf(diag.V204, a.Name, "argument "+quote(a.Name.Name), "the call to "+what)
				continue
			}
		} else if idx >= len(params) {
			e.errorf(diag.V302, a, what, "it takes "+strconv.Itoa(len(params))+" arguments, not "+strconv.Itoa(len(x.Args)))
			break
		}
		v := e.expr(a.Value, params[idx].Type)
		e.assign(v, params[idx].Type, a.Value, "argument "+params[idx].Name+" of "+what)
		out[idx] = v
	}
	var missing []string
	for i, p := range params {
		if out[i] != nil {
			continue
		}
		if p.Default != nil {
			out[i] = p.Default
			continue
		}
		missing = append(missing, quote(p.Name))
		out[i] = e.invalid(x)
	}
	if len(missing) > 0 {
		e.errorf(diag.V302, x, what, "missing "+strings.Join(missing, ", "))
	}
	return out
}

// positional checks the arguments of a built-in, which takes n of them
// without names.
func (e *env) positional(x *ast.CallExpr, what string, n int) ([]ir.Expr, bool) {
	if len(x.Args) != n {
		e.errorf(diag.V302, x, what, "it takes "+strconv.Itoa(n)+" argument"+plural(n)+", not "+strconv.Itoa(len(x.Args)))
		return nil, false
	}
	var out []ir.Expr
	for _, a := range x.Args {
		if a.Name != nil {
			e.errorf(diag.V302, a.Name, what, "its arguments have no names")
			return nil, false
		}
		out = append(out, e.expr(a.Value, nil))
	}
	return out, true
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (e *env) builtin(x *ast.CallExpr, name string) ir.Expr {
	out := &ir.Builtin{Name: name}
	out.P = e.pos(x)
	switch name {
	case "now":
		if _, ok := e.positional(x, "now", 0); !ok {
			return e.invalid(x)
		}
		out.T = ir.Basic(ir.Time)
	case "len":
		args, ok := e.positional(x, "len", 1)
		if !ok {
			return e.invalid(x)
		}
		switch t := args[0].Type(); {
		case t.IsText(), t.Kind == ir.List, t.Kind == ir.Query, t.Kind == ir.JSON, t.Kind == ir.Invalid:
		default:
			e.errorf(diag.V301, x.Args[0].Value, "len takes Text or a list, not "+t.String())
		}
		out.Args, out.T = args, ir.Basic(ir.Int)
	case "str":
		args, ok := e.positional(x, "str", 1)
		if !ok {
			return e.invalid(x)
		}
		if k := args[0].Type().Kind; k == ir.Empty || k == ir.ModelName {
			e.errorf(diag.V301, x.Args[0].Value, "str needs a value")
		}
		out.Args, out.T = args, ir.Basic(ir.Text)
	case "login_as":
		if e.kind != inTest {
			e.errorf(diag.V405, x, "login_as")
		}
		if e.c.auth == nil {
			e.errorf(diag.V202, x.Fun, "'login_as'", "; login_as needs an auth declaration, such as auth User")
			return e.invalid(x)
		}
		args, ok := e.positional(x, "login_as", 1)
		if !ok {
			return e.invalid(x)
		}
		e.assign(args[0], ir.ModelOf(e.c.auth.ir), x.Args[0].Value, "the argument of login_as")
		out.Args, out.T = args, ir.Basic(ir.Empty)
	}
	return out
}

func (e *env) request(x *ast.HTTPExpr) ir.Expr {
	method := strings.ToUpper(x.Method.String())
	if e.kind != inTest {
		e.errorf(diag.V402, x, method+" "+e.f.Text(x.Path.Pos(), x.Path.End()))
	}
	out := &ir.Request{Method: method}
	out.T, out.P = ir.Basic(ir.Response), e.pos(x)
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			k := &ir.Const{Value: text.String()}
			k.T, k.P = ir.Basic(ir.Text), out.P
			out.Path = append(out.Path, k)
			text.Reset()
		}
	}
	if len(x.Path.Segments) == 0 {
		text.WriteByte('/')
	}
	for _, s := range x.Path.Segments {
		text.WriteByte('/')
		if s.X == nil {
			text.WriteString(s.Text)
			continue
		}
		flush()
		v := e.expr(s.X, nil)
		if k := v.Type().Kind; k == ir.Empty || k == ir.ModelName {
			e.errorf(diag.V301, s.X, "a path segment needs a value")
		}
		out.Path = append(out.Path, v)
	}
	flush()
	for _, a := range x.Args {
		if a.Name == nil {
			e.errorf(diag.V302, a, method+" "+e.f.Text(x.Path.Pos(), x.Path.End()), "a request takes named fields, as in post /books (title: \"Dune\")")
			continue
		}
		v := e.expr(a.Value, nil)
		if k := v.Type().Kind; k == ir.Empty || k == ir.ModelName {
			e.errorf(diag.V301, a.Value, "field "+a.Name.Name+" of the request needs a value")
		}
		out.Args = append(out.Args, ir.Arg{Name: a.Name.Name, Value: v})
	}
	return out
}
