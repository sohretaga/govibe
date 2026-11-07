package gogen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/sqlgen"
)

// body generates the code of one route or fn. Statements go to w;
// expressions return Go expressions, writing the statements they need
// first.
type body struct {
	g     *gen
	w     *strings.Builder
	route *ir.Route // nil in a fn
	fn    *ir.Func  // nil in a route
	temps int
	names map[*ir.Var]string
	taken map[string]bool
	used  map[*ir.Var]bool // locals the code reads
}

func newBody(g *gen) *body {
	return &body{g: g, w: &strings.Builder{}, names: map[*ir.Var]string{}, taken: map[string]bool{}, used: map[*ir.Var]bool{}}
}

func (f *body) line(format string, args ...any) {
	fmt.Fprintf(f.w, format+"\n", args...)
}

// failOn writes the early return of a failed step.
func (f *body) failOn() {
	if f.route == nil {
		// Pure fns cannot fail; sema rejects queries in them.
		panic("gogen: a fn cannot fail")
	}
	f.line("if err != nil {\nreturn err\n}")
}

func (f *body) temp() string {
	f.temps++
	return "t" + strconv.Itoa(f.temps)
}

// local returns the Go name of a local, giving it one on first use.
func (f *body) local(v *ir.Var) string {
	if n, ok := f.names[v]; ok {
		return n
	}
	n := v.Name
	if isReserved(n) {
		n += "_"
	}
	for i := 2; f.taken[n]; i++ {
		n = v.Name + "_" + strconv.Itoa(i)
	}
	f.taken[n] = true
	f.names[v] = n
	return n
}

// markUsed records the locals that b reads, so unread ones are not
// declared: Go rejects an unused variable.
func (f *body) markUsed(b *ir.Block, values ...ir.Expr) {
	mark := func(e ir.Expr) bool {
		if r, ok := e.(*ir.VarRef); ok {
			f.used[r.Var] = true
		}
		return true
	}
	ir.InspectBlock(b, mark)
	for _, v := range values {
		ir.Inspect(v, mark)
	}
}

// capture runs gen with a fresh statement buffer and returns the
// statements it wrote and its result.
func (f *body) capture(gen func() string) (stmts, x string) {
	saved := f.w
	f.w = &strings.Builder{}
	x = gen()
	stmts = f.w.String()
	f.w = saved
	return stmts, x
}

// convert adapts the Go expression x of type from to type to, under the
// conversion rules of the spec.
func convert(x string, from, to *ir.Type) string {
	switch {
	case to == nil || from.Kind == ir.Invalid || ir.Identical(from, to):
		return x
	case from.Kind == ir.None:
		return "nil"
	case to.Kind == ir.Optional && from.Kind == ir.Optional:
		if from.Elem.Kind == ir.Int && to.Elem.Kind == ir.Float {
			return "vibe.WidenPtr(" + x + ")"
		}
		return x
	case to.Kind == ir.Optional:
		if from.Kind == ir.Record {
			return x
		}
		return "vibe.Ptr(" + convert(x, from, to.Elem) + ")"
	case to.Kind == ir.Float && from.Kind == ir.Int:
		return "float64(" + x + ")"
	case to.Kind == ir.List && from.Kind == ir.List && from.Elem.Kind == ir.Int && to.Elem.Kind == ir.Float:
		return "vibe.WidenList(" + x + ")"
	}
	return x
}

// value generates e converted to type want.
func (f *body) value(e ir.Expr, want *ir.Type) string {
	return convert(f.expr(e), e.Type(), want)
}

// constant returns a constant as a typed Go expression.
func constant(k *ir.Const) string {
	t := k.Type()
	if k.Value == nil {
		return "nil"
	}
	x := ""
	switch v := k.Value.(type) {
	case bool:
		x = strconv.FormatBool(v)
	case string:
		x = strconv.Quote(v)
	case int64:
		x = "int64(" + strconv.FormatInt(v, 10) + ")"
		if elem(t).Kind == ir.Money {
			x = "vibe.Money(" + strconv.FormatInt(v, 10) + ")"
		}
	case float64:
		x = "float64(" + strconv.FormatFloat(v, 'g', -1, 64) + ")"
	default:
		panic(fmt.Sprintf("gogen: constant of type %T", k.Value))
	}
	return convert(x, elem(t), t)
}

func (f *body) expr(e ir.Expr) string {
	switch e := e.(type) {
	case *ir.Const:
		if e.Type().Kind == ir.ModelName {
			panic("gogen: a model name is not a value")
		}
		return constant(e)
	case *ir.VarRef:
		n := f.local(e.Var)
		if e.Var.Type.Kind == ir.Optional && e.Type().Kind != ir.Optional && e.Type().Kind != ir.Record {
			return "*" + n
		}
		return n
	case *ir.Unary:
		if e.Op == "not" {
			return "!" + paren(f.expr(e.X))
		}
		return "-" + paren(f.expr(e.X))
	case *ir.Binary:
		return f.binary(e)
	case *ir.Interp:
		return f.interp(e)
	case *ir.ListExpr:
		elems := make([]string, len(e.Elems))
		for i, x := range e.Elems {
			elems[i] = f.value(x, e.Type().Elem)
		}
		return goType(e.Type()) + "{" + strings.Join(elems, ", ") + "}"
	case *ir.FieldRef:
		return paren(f.expr(e.X)) + "." + f.g.fields[e.Field]
	case *ir.Call:
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			args[i] = f.value(a, e.Func.Params[i].Type)
		}
		if e.Func.Extern {
			// An extern may fail; its error answers internal, or the
			// status of a vibe.Fail.
			t := f.temp()
			f.line("%s, err := %s(%s)", t, externName(e.Func), strings.Join(append([]string{"c.Context()"}, args...), ", "))
			f.failOn()
			return t
		}
		return fnName(e.Func) + "(" + strings.Join(args, ", ") + ")"
	case *ir.Builtin:
		return f.builtin(e)
	case *ir.QueryExpr:
		return f.query(e)
	case *ir.Find:
		t := f.temp()
		f.line("%s, err := find%sBy%s(c, %s)", t, e.Model.Name, f.g.fields[e.Field], f.value(e.Value, elem(e.Field.Type)))
		f.failOn()
		return t
	case *ir.Add:
		return f.add(e)
	case *ir.Update:
		return f.update(e)
	case *ir.Delete:
		f.line("if err := delete%s(c, %s); err != nil {\nreturn err\n}", e.X.Type().Model.Name, f.expr(e.X))
		return ""
	case *ir.Me:
		return "me"
	case *ir.Request, *ir.Member, *ir.StatusName:
		f.g.unsupported(e.Position(), "requests outside tests")
	}
	panic(fmt.Sprintf("gogen: expression %T", e))
}

// paren wraps x in parentheses unless it is one operand already: a name,
// literal, call, index or selector with no operator outside brackets and
// string literals.
func paren(x string) string {
	if x == "" || strings.ContainsRune("!-*&", rune(x[0])) {
		return "(" + x + ")"
	}
	depth := 0
	for i := 0; i < len(x); i++ {
		switch x[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ' ':
			if depth == 0 {
				return "(" + x + ")"
			}
		case '"':
			for i++; i < len(x) && x[i] != '"'; i++ {
				if x[i] == '\\' {
					i++
				}
			}
		}
	}
	return x
}

func (f *body) binary(e *ir.Binary) string {
	switch e.Op {
	case "and", "or":
		return f.logic(e)
	case "+", "-", "*", "/", "%":
		return f.arith(e)
	}
	return f.compare(e)
}

// logic keeps the short circuit of and and or: when the right side needs
// statements, they run only if the left side does not decide.
func (f *body) logic(e *ir.Binary) string {
	op := map[string]string{"and": "&&", "or": "||"}[e.Op]
	x := f.expr(e.X)
	stmts, y := f.capture(func() string { return f.expr(e.Y) })
	if stmts == "" {
		return paren(x) + " " + op + " " + paren(y)
	}
	t := f.temp()
	f.line("%s := %s", t, x)
	if e.Op == "and" {
		f.line("if %s {", t)
	} else {
		f.line("if !%s {", t)
	}
	f.w.WriteString(stmts)
	f.line("%s = %s\n}", t, y)
	return t
}

func (f *body) arith(e *ir.Binary) string {
	t := e.Type()
	xt, yt := e.X.Type(), e.Y.Type()
	x, y := f.expr(e.X), f.expr(e.Y)
	switch {
	case t.Kind == ir.Float:
		x, y = convert(x, xt, t), convert(y, yt, t)
	case t.Kind == ir.Money && xt.Kind == ir.Int:
		x = "vibe.Money(" + x + ")"
	case t.Kind == ir.Money && yt.Kind == ir.Int:
		y = "vibe.Money(" + y + ")"
	}
	return paren(x) + " " + e.Op + " " + paren(y)
}

func (f *body) compare(e *ir.Binary) string {
	xt, yt := e.X.Type(), e.Y.Type()
	x, y := f.expr(e.X), f.expr(e.Y)
	if e.Op == "contains" {
		return "vibe.Contains(" + x + ", " + y + ")"
	}
	// Bring Int and Float to Float.
	if elem(xt).Kind == ir.Float && elem(yt).Kind == ir.Int && yt.Kind != ir.Optional {
		y = "float64(" + y + ")"
	}
	if elem(yt).Kind == ir.Float && elem(xt).Kind == ir.Int && xt.Kind != ir.Optional {
		x = "float64(" + x + ")"
	}
	xo, yo := xt.Kind == ir.Optional, yt.Kind == ir.Optional
	xn, yn := xt.Kind == ir.None, yt.Kind == ir.None
	eq := e.Op == "==" || e.Op == "!="
	not := ""
	if e.Op == "!=" {
		not = "!"
	}
	switch {
	case eq && (xn || yn):
		if xn && yn {
			return strconv.FormatBool(e.Op == "==")
		}
		if xn {
			x = y
		}
		return paren(x) + " " + e.Op + " nil"
	case eq && xo && yo:
		return not + "vibe.EqPtrs(" + x + ", " + y + ")"
	case eq && xo:
		return not + "vibe.EqPtr(" + x + ", " + y + ")"
	case eq && yo:
		return not + "vibe.EqPtr(" + y + ", " + x + ")"
	case elem(xt).Kind == ir.Time:
		switch e.Op {
		case "==":
			return paren(x) + ".Equal(" + y + ")"
		case "!=":
			return "!" + paren(x) + ".Equal(" + y + ")"
		case "<":
			return paren(x) + ".Before(" + y + ")"
		case ">":
			return paren(x) + ".After(" + y + ")"
		case "<=":
			return "!" + paren(x) + ".After(" + y + ")"
		case ">=":
			return "!" + paren(x) + ".Before(" + y + ")"
		}
	}
	return paren(x) + " " + e.Op + " " + paren(y)
}

func (f *body) interp(e *ir.Interp) string {
	if len(e.Parts) == 0 {
		return `""`
	}
	parts := make([]string, len(e.Parts))
	for i, p := range e.Parts {
		if p.Type().IsText() {
			parts[i] = f.expr(p)
		} else {
			parts[i] = "vibe.Str(" + f.expr(p) + ")"
		}
	}
	return strings.Join(parts, " + ")
}

func (f *body) builtin(e *ir.Builtin) string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = f.expr(a)
	}
	switch e.Name {
	case "now":
		return "vibe.Now()"
	case "len":
		if e.Args[0].Type().IsText() {
			return "vibe.Len(" + args[0] + ")"
		}
		return "int64(len(" + args[0] + "))"
	case "str":
		if e.Args[0].Type().IsText() {
			return args[0]
		}
		return "vibe.Str(" + args[0] + ")"
	case "lower":
		return "strings.ToLower(" + args[0] + ")"
	case "upper":
		return "strings.ToUpper(" + args[0] + ")"
	case "trim":
		return "strings.TrimSpace(" + args[0] + ")"
	case "starts_with":
		return "strings.HasPrefix(" + args[0] + ", " + args[1] + ")"
	case "ends_with":
		return "strings.HasSuffix(" + args[0] + ", " + args[1] + ")"
	case "login_as":
		f.g.unsupported(e.Position(), "login_as outside tests")
	}
	panic("gogen: builtin " + e.Name)
}

// query runs a query site and returns its result.
func (f *body) query(q *ir.QueryExpr) string {
	plan := sqlgen.Select(q)
	args := []string{"c"}
	for _, p := range plan.Params {
		args = append(args, f.param(p))
	}
	run := f.g.site(q, plan)
	t := f.temp()
	f.line("%s, err := %s(%s)", t, run, strings.Join(args, ", "))
	f.failOn()
	return t
}

// param returns a query parameter as the database stores it.
func (f *body) param(p sqlgen.Param) string {
	x := f.expr(p.Expr)
	t := p.Expr.Type()
	switch {
	case p.Use == sqlgen.Limit:
		return "vibe.Limit(" + x + ")"
	case p.Use == sqlgen.Offset:
		return "vibe.Offset(" + x + ")"
	case t.Kind == ir.Time:
		return "vibe.TimeArg(" + x + ")"
	case t.Kind == ir.Optional && t.Elem.Kind == ir.Time:
		return "vibe.OptTimeArg(" + x + ")"
	}
	return x
}

// add creates a record: given fields, defaults for the others.
func (f *body) add(e *ir.Add) string {
	m := e.Model
	given := map[*ir.Field]string{}
	for _, v := range e.Values {
		given[v.Field] = f.value(v.Value, v.Field.Type)
	}
	var inits []string
	for _, fl := range m.Fields {
		x, ok := given[fl]
		if !ok && fl.Default != nil && fl.Default.Value != nil {
			x, ok = constant(fl.Default), true
		}
		if ok {
			inits = append(inits, f.g.fields[fl]+": "+x)
		}
	}
	t, errs := f.temp(), f.temp()
	f.line("var %s vibe.Errors", errs)
	f.line("%s, err := insert%s(c, &model.%s{%s}, &%s)", t, m.Name, m.Name, strings.Join(inits, ", "), errs)
	f.failOn()
	return t
}

// update changes a copy of a record, so a local that holds the record
// keeps its values.
func (f *body) update(e *ir.Update) string {
	m := e.X.Type().Model
	x := f.expr(e.X)
	t, errs := f.temp(), f.temp()
	f.line("%s := *%s", t, x)
	for _, v := range e.Values {
		f.line("%s.%s = %s", t, f.g.fields[v.Field], f.value(v.Value, v.Field.Type))
	}
	f.line("var %s vibe.Errors", errs)
	res := f.temp()
	f.line("%s, err := update%s(c, &%s, &%s)", res, m.Name, t, errs)
	f.failOn()
	return res
}

// fnName returns the Go name of a fn; a feature's fn carries the feature
// name, since two features may each have a fn of the same name.
func fnName(fn *ir.Func) string { return "fn" + fn.Feature + pascal(fn.Name) }

// externName returns the Go function of an extern fn in package ext:
// charge_card is ext.ChargeCard.
func externName(fn *ir.Func) string { return "ext." + pascal(fn.Name) }
