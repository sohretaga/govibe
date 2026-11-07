package ir

// Inspect calls f for e and, while f returns true, for every expression
// inside it, depth first. Query conditions, limits and offsets count as
// inside their query.
func Inspect(e Expr, f func(Expr) bool) {
	if e == nil || !f(e) {
		return
	}
	each := func(xs ...Expr) {
		for _, x := range xs {
			Inspect(x, f)
		}
	}
	switch e := e.(type) {
	case *Unary:
		each(e.X)
	case *Binary:
		each(e.X, e.Y)
	case *Interp:
		each(e.Parts...)
	case *ListExpr:
		each(e.Elems...)
	case *FieldRef:
		each(e.X)
	case *Member:
		each(e.X)
	case *Call:
		each(e.Args...)
	case *Builtin:
		each(e.Args...)
	case *QueryExpr:
		each(e.Where...)
		each(e.Limit, e.Offset)
	case *Find:
		each(e.Value)
	case *Add:
		for _, v := range e.Values {
			each(v.Value)
		}
	case *Update:
		each(e.X)
		for _, v := range e.Values {
			each(v.Value)
		}
	case *Delete:
		each(e.X)
	case *Request:
		each(e.Path...)
		for _, a := range e.Args {
			each(a.Value)
		}
	}
}

// InspectBlock calls Inspect for every expression in the statements of
// b, nested blocks included.
func InspectBlock(b *Block, f func(Expr) bool) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		inspectStmt(s, f)
	}
}

func inspectStmt(s Stmt, f func(Expr) bool) {
	switch s := s.(type) {
	case *Block:
		InspectBlock(s, f)
	case *Let:
		Inspect(s.Value, f)
	case *If:
		Inspect(s.Cond, f)
		InspectBlock(s.Then, f)
		if s.Else != nil {
			inspectStmt(s.Else, f)
		}
	case *Return:
		Inspect(s.Value, f)
	case *Fail:
		Inspect(s.Message, f)
	case *Expect:
		Inspect(s.Cond, f)
	case *ExprStmt:
		Inspect(s.X, f)
	}
}
