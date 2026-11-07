package gogen

import (
	"fmt"

	"github.com/sohretaga/govibe/internal/ir"
)

// statusConst returns the vibe constant of a status name.
func statusConst(name string) string {
	if name == "ok" {
		return "vibe.OK"
	}
	return "vibe." + pascal(name)
}

// block writes the statements of b up to the first that ends the path:
// Go rejects code after it.
func (f *body) block(b *ir.Block) {
	for _, s := range b.Stmts {
		f.stmt(s)
		if stmtTerminates(s) {
			return
		}
	}
}

// stmtPos returns where a statement starts in its .vibe file.
func stmtPos(s ir.Stmt) ir.Pos {
	switch s := s.(type) {
	case *ir.Let:
		return s.Value.Position()
	case *ir.If:
		return s.Cond.Position()
	case *ir.Return:
		return s.Pos
	case *ir.Fail:
		return s.Pos
	case *ir.Expect:
		return s.Pos
	case *ir.ExprStmt:
		return s.X.Position()
	}
	return ir.Pos{}
}

// stmt writes a statement between marks that map its Go code to its
// .vibe line.
func (f *body) stmt(s ir.Stmt) {
	p := stmtPos(s)
	saved := f.pos
	f.pos = p
	f.mark(p)
	f.stmtCode(s)
	f.unmark(p)
	f.pos = saved
}

func (f *body) stmtCode(s ir.Stmt) {
	switch s := s.(type) {
	case *ir.Block:
		f.line("{")
		f.block(s)
		f.line("}")
	case *ir.Let:
		x := f.value(s.Value, s.Var.Type)
		if !f.used[s.Var] {
			if x != "" {
				f.line("_ = %s", x)
			}
			return
		}
		f.line("%s := %s", f.local(s.Var), x)
	case *ir.If:
		f.ifStmt(s)
	case *ir.Return:
		f.ret(s)
	case *ir.Fail:
		msg := f.value(s.Message, ir.Basic(ir.Text))
		if f.test != nil {
			f.line("return vibe.Stop(%q, %d, %s)", s.Pos.File, s.Pos.Line, msg)
			return
		}
		f.line("return vibe.Fail(%s, %s)", statusConst(s.Status), msg)
	case *ir.ExprStmt:
		if x := f.expr(s.X); x != "" {
			f.line("_ = %s", x)
		}
	case *ir.Expect:
		if f.test == nil {
			panic(&Unsupported{Pos: s.Pos, What: "expect outside tests"})
		}
		f.expect(s)
	default:
		panic(fmt.Sprintf("gogen: statement %T", s))
	}
}

func (f *body) ifStmt(s *ir.If) {
	f.ifChain(s, f.value(s.Cond, ir.Basic(ir.Bool)), "if")
}

// ifChain writes an if whose condition is already generated; kw is "if"
// or "} else if".
func (f *body) ifChain(s *ir.If, cond, kw string) {
	f.line("%s %s {", kw, cond)
	f.block(s.Then)
	switch els := s.Else.(type) {
	case *ir.Block:
		f.line("} else {")
		f.block(els)
		f.line("}")
	case *ir.If:
		saved := f.pos
		f.pos = els.Cond.Position()
		stmts, c := f.capture(func() string { return f.value(els.Cond, ir.Basic(ir.Bool)) })
		f.pos = saved
		if stmts == "" {
			f.ifChain(els, c, "} else if")
			return
		}
		// The condition needs statements first, so the else holds them
		// and a new if, marked with the line of the condition.
		f.line("} else {")
		f.mark(els.Cond.Position())
		f.w.WriteString(stmts)
		f.ifChain(els, c, "if")
		f.unmark(els.Cond.Position())
		f.line("}")
	default:
		f.line("}")
	}
}

// ret writes a return: a fn returns its value; a route answers with it;
// a test ends.
func (f *body) ret(s *ir.Return) {
	if f.test != nil {
		f.line("return nil")
		return
	}
	if f.fn != nil {
		f.line("return %s", f.value(s.Value, f.fn.Result))
		return
	}
	f.answer(s.Value)
}

// answer writes the response of a route: ok with the value, or empty.
func (f *body) answer(v ir.Expr) {
	result := f.route.Result
	if v == nil || result.Kind == ir.Empty {
		if v != nil {
			if x := f.expr(v); x != "" {
				f.line("_ = %s", x)
			}
		}
		f.line("return c.Empty()")
		return
	}
	f.line("return c.OK(%s)", f.value(v, result))
}
