package sema

import (
	"strconv"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/token"
)

type context int

const (
	inFn context = iota
	inRoute
	inTest
)

// env is the state of checking one function, route or test body.
type env struct {
	c    *checker
	f    *token.File
	kind context
	fn   *function // the fn being checked, for inFn
	feat *feature  // the enclosing feature, or nil
	name string    // how messages name the body, such as "route GET /books"

	scope  *scope
	usesMe bool
	// where is the model whose fields bare names resolve to inside a
	// query condition.
	where *ir.Model
	// allowColumn is set just before checking a position where a field of
	// the where model may appear: a condition or a comparison operand.
	allowColumn bool
	returns     []ret
}

type ret struct {
	node ast.Node
	t    *ir.Type // nil for a bare return
}

type binding struct {
	v    *ir.Var
	t    *ir.Type // the type at this point, narrowed by if x != none
	line int
}

type scope struct {
	parent *scope
	names  map[string]*binding
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, names: map[string]*binding{}}
}

func (s *scope) lookup(name string) *binding {
	for ; s != nil; s = s.parent {
		if b, ok := s.names[name]; ok {
			return b
		}
	}
	return nil
}

// visible returns every name in scope, for suggestions.
func (s *scope) visible() []string {
	var names []string
	for ; s != nil; s = s.parent {
		for n := range s.names {
			names = append(names, n)
		}
	}
	return names
}

func (e *env) diag(code diag.Code, n ast.Node, args ...any) diag.Diagnostic {
	return e.c.diagAt(e.f, code, n, args...)
}

func (e *env) errorf(code diag.Code, n ast.Node, args ...any) { e.c.add(e.diag(code, n, args...)) }

func (e *env) pos(n ast.Node) ir.Pos { return pos(e.f, n.Pos()) }

func (e *env) line(n ast.Node) int { return e.f.Line(n.Pos()) }

func (e *env) declare(id *ast.Ident, v *ir.Var) {
	if prev, ok := e.scope.names[id.Name]; ok {
		e.errorf(diag.V203, id, quote(id.Name), strconv.Itoa(prev.line))
		return
	}
	e.scope.names[id.Name] = &binding{v: v, t: v.Type, line: e.line(id)}
}

// paramScope returns the outermost scope of a body, holding params.
func (e *env) paramScope(params []*ir.Param) *scope {
	sc := newScope(nil)
	for _, p := range params {
		if p.Var != nil {
			sc.names[p.Name] = &binding{v: p.Var, t: p.Type, line: p.Pos.Line}
		}
	}
	return sc
}

// effect reports V310 when a fn does what only routes and tests may do.
func (e *env) effect(n ast.Node, what string) {
	if e.kind == inFn {
		e.errorf(diag.V310, n, e.fn.ir.Name, what)
	}
}

func (c *checker) checkBodies() {
	for _, fn := range c.fnList {
		c.checkFunc(fn)
	}
	for _, r := range c.routes {
		if r.ast != nil {
			c.checkRoute(r)
		}
	}
	for _, t := range c.tests {
		e := &env{c: c, f: t.file, kind: inTest, feat: t.feat, name: "test " + quote(t.ir.Name)}
		t.ir.Body, _ = e.block(t.ast.Body, newScope(nil))
	}
}

func (c *checker) checkFunc(fn *function) {
	e := &env{c: c, f: fn.file, kind: inFn, fn: fn, feat: fn.feature, name: "fn " + fn.ir.Name}
	body, term := e.block(fn.fn.Body, e.paramScope(fn.ir.Params))
	fn.ir.Body = body
	if !term && fn.ir.Result.Kind != ir.Invalid {
		e.missingReturn(fn.fn.Body, fn.ir.Result)
	}
}

// missingReturn reports V306 at the closing brace of b. When the last
// statement is an expression of the right type, the fix returns it.
func (e *env) missingReturn(b *ast.Block, result *ir.Type) {
	d := diag.New(e.f, diag.V306, b.Rbrace, b.Rbrace+1, e.name)
	if n := len(b.Stmts); n > 0 {
		if s, ok := b.Stmts[n-1].(*ast.ExprStmt); ok {
			d = d.WithFix("Return the last value", diag.Insert(e.f, s.Pos(), "return "))
		}
	}
	e.c.add(d)
}

func (c *checker) checkRoute(r *route) {
	e := &env{c: c, f: r.file, kind: inRoute, feat: r.feat, name: "route " + r.ir.Method + " " + r.ir.Path}
	sc := e.paramScope(r.ir.Params)
	var result *ir.Type
	if r.ast.Value != nil {
		e.scope = sc
		v := e.expr(r.ast.Value, nil)
		if v.Type().Kind == ir.ModelName {
			e.errorf(diag.V301, r.ast.Value, v.Type().Model.Name+" is a model, not a value; use "+v.Type().Model.Name+".all()")
		}
		r.ir.Value = v
		result = v.Type().Value()
	} else {
		body, term := e.block(r.ast.Body, sc)
		r.ir.Body = body
		result = e.routeResult(r.ast.Body, term)
	}
	r.ir.Result = result
	r.ir.Status = "ok"
	if result.Kind == ir.Empty {
		r.ir.Status = "empty"
	}
	r.ir.Login = e.usesMe
}

// routeResult unifies the types of a route's returns.
func (e *env) routeResult(body *ast.Block, term bool) *ir.Type {
	var first *ret
	for i := range e.returns {
		if e.returns[i].t != nil && e.returns[i].t.Kind != ir.Invalid {
			first = &e.returns[i]
			break
		}
	}
	if first == nil {
		return ir.Basic(ir.Empty)
	}
	result := first.t
	for _, r := range e.returns {
		switch {
		case r.t == nil:
			e.errorf(diag.V301, r.node, "this return has no value, but "+e.name+" returns "+result.String()+" at line "+strconv.Itoa(e.line(first.node)))
		case r.t.Kind == ir.Invalid || r == *first:
		case r.t.Kind == ir.None && result.Kind == ir.Optional:
		case result.Kind == ir.None && r.t.Kind == ir.Optional:
			result = r.t
		case !ir.Identical(r.t, result):
			e.errorf(diag.V301, r.node, e.name+" returns "+r.t.String()+" here but "+result.String()+" at line "+strconv.Itoa(e.line(first.node))+"; every return has one type")
		}
	}
	if !term {
		e.missingReturn(body, result)
	}
	return result
}

func (e *env) block(b *ast.Block, sc *scope) (*ir.Block, bool) {
	saved := e.scope
	e.scope = sc
	defer func() { e.scope = saved }()
	out := &ir.Block{}
	term := false
	for _, s := range b.Stmts {
		st, t := e.stmt(s)
		if st != nil {
			out.Stmts = append(out.Stmts, st)
		}
		term = term || t
	}
	return out, term
}

// stmt checks a statement and reports whether it ends its path.
func (e *env) stmt(s ast.Stmt) (ir.Stmt, bool) {
	switch s := s.(type) {
	case *ast.LetStmt:
		return e.let(s), false
	case *ast.IfStmt:
		return e.ifStmt(s)
	case *ast.ReturnStmt:
		return e.ret(s), true
	case *ast.FailStmt:
		return e.fail(s), true
	case *ast.ExpectStmt:
		if e.kind != inTest {
			e.errorf(diag.V405, s, "expect")
		}
		return &ir.Expect{Cond: e.cond(s.Cond, false), Text: e.f.Text(s.Cond.Pos(), s.Cond.End()), Pos: e.pos(s)}, false
	case *ast.ExprStmt:
		return &ir.ExprStmt{X: e.expr(s.X, nil)}, false
	}
	return nil, false
}

func (e *env) let(s *ast.LetStmt) ir.Stmt {
	v := e.expr(s.Value, nil)
	t := v.Type()
	switch t.Kind {
	case ir.Empty:
		e.errorf(diag.V301, s.Value, "let "+s.Name.Name+" needs a value, but this has none")
		t = ir.Basic(ir.Invalid)
	case ir.ModelName:
		e.errorf(diag.V301, s.Value, t.Model.Name+" is a model, not a value; use "+t.Model.Name+".all() for its records")
		t = ir.Basic(ir.Invalid)
	case ir.None:
		e.errorf(diag.V301, s.Value, "let "+s.Name.Name+" cannot be none: a let takes the type of its value")
		t = ir.Basic(ir.Invalid)
	}
	variable := &ir.Var{Name: s.Name.Name, Type: t.Value(), Pos: e.pos(s.Name)}
	e.declare(s.Name, variable)
	return &ir.Let{Var: variable, Value: v}
}

func (e *env) ifStmt(s *ast.IfStmt) (ir.Stmt, bool) {
	cond := e.cond(s.Cond, false)
	then := newScope(e.scope)
	for name, t := range e.narrowings(s.Cond) {
		b := e.scope.lookup(name)
		then.names[name] = &binding{v: b.v, t: t, line: b.line}
	}
	thenBlock, t1 := e.block(s.Then, then)
	out := &ir.If{Cond: cond, Then: thenBlock}
	t2 := false
	switch els := s.Else.(type) {
	case *ast.Block:
		b, t := e.block(els, newScope(e.scope))
		out.Else, t2 = b, t
	case *ast.IfStmt:
		st, t := e.ifStmt(els)
		out.Else, t2 = st, t
	}
	return out, s.Else != nil && t1 && t2
}

// narrowings finds the optional locals that cond tests with x != none,
// alone or joined with and, and returns their narrowed types.
func (e *env) narrowings(cond ast.Expr) map[string]*ir.Type {
	out := map[string]*ir.Type{}
	var walk func(x ast.Expr)
	walk = func(x ast.Expr) {
		switch x := x.(type) {
		case *ast.ParenExpr:
			walk(x.X)
		case *ast.BinaryExpr:
			if x.Op == token.And {
				walk(x.X)
				walk(x.Y)
				return
			}
			if x.Op != token.NotEq {
				return
			}
			id, ok := x.X.(*ast.Ident)
			other := x.Y
			if !ok {
				id, ok = x.Y.(*ast.Ident)
				other = x.X
			}
			lit, isLit := other.(*ast.BasicLit)
			if !ok || !isLit || lit.Kind != token.None {
				return
			}
			if b := e.scope.lookup(id.Name); b != nil && b.t.Kind == ir.Optional {
				out[id.Name] = b.t.Elem
			}
		}
	}
	walk(cond)
	return out
}

func (e *env) ret(s *ast.ReturnStmt) ir.Stmt {
	out := &ir.Return{Pos: e.pos(s)}
	var want *ir.Type
	if e.kind == inFn {
		want = e.fn.ir.Result
	}
	if s.Value != nil {
		out.Value = e.expr(s.Value, want)
	}
	switch e.kind {
	case inFn:
		if out.Value == nil {
			e.errorf(diag.V301, s, e.name+" must return "+want.String())
		} else {
			e.assign(out.Value, want, s.Value, "the result of "+e.name)
		}
	case inRoute:
		r := ret{node: s}
		if out.Value != nil {
			r.t = out.Value.Type().Value()
			if r.t.Kind == ir.Empty {
				r.t = nil
			}
		}
		e.returns = append(e.returns, r)
	case inTest:
		if out.Value != nil {
			e.errorf(diag.V301, s.Value, "a test does not return a value")
		}
	}
	return out
}

// statusCodes maps the HTTP codes that fail may use to status names.
var statusCodes = map[string]string{"401": "login_required", "403": "forbidden", "404": "not_found", "409": "conflict", "422": "invalid"}

func (e *env) fail(s *ast.FailStmt) ir.Stmt {
	e.effect(s, "fail")
	out := &ir.Fail{Status: "invalid", Pos: e.pos(s)}
	if s.Status != nil {
		out.Status = s.Status.Name
	}
	if s.Code != nil {
		name, ok := statusCodes[s.Code.Value]
		if ok {
			d := e.diag(diag.V312, s.Code, s.Code.Value, "; write "+name)
			e.c.add(d.WithFix("Use "+quote(name), diag.Replace(e.f, s.Code.Pos(), s.Code.End(), name)))
			out.Status = name
		} else {
			e.errorf(diag.V312, s.Code, s.Code.Value, "; use invalid, login_required, forbidden, not_found or conflict")
		}
	}
	out.Message = e.expr(s.Message, ir.Basic(ir.Text))
	return out
}
