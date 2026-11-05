package format

import (
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/lexer"
	"github.com/sohretaga/govibe/internal/token"
)

// expr prints an expression on one line.
func expr(e ast.Expr) string {
	var b strings.Builder
	writeExpr(&b, e)
	return b.String()
}

func writeExpr(b *strings.Builder, e ast.Expr) {
	switch e := e.(type) {
	case *ast.Ident:
		b.WriteString(e.Name)
	case *ast.BasicLit:
		b.WriteString(e.Value)
	case *ast.MeExpr:
		b.WriteString("me")
	case *ast.StringLit:
		b.WriteByte('"')
		for _, part := range e.Parts {
			if part.X != nil {
				b.WriteByte('{')
				writeExpr(b, part.X)
				b.WriteByte('}')
				continue
			}
			quoted := lexer.Quote(part.Text)
			b.WriteString(quoted[1 : len(quoted)-1])
		}
		b.WriteByte('"')
	case *ast.ListLit:
		b.WriteByte('[')
		for i, x := range e.Elems {
			if i > 0 {
				b.WriteString(", ")
			}
			writeExpr(b, x)
		}
		b.WriteByte(']')
	case *ast.ParenExpr:
		b.WriteByte('(')
		writeExpr(b, e.X)
		b.WriteByte(')')
	case *ast.UnaryExpr:
		b.WriteString(e.Op.String())
		if e.Op == token.Not {
			b.WriteByte(' ')
		}
		writeExpr(b, e.X)
	case *ast.BinaryExpr:
		writeExpr(b, e.X)
		b.WriteString(" " + e.Op.String() + " ")
		writeExpr(b, e.Y)
	case *ast.SelectorExpr:
		writeExpr(b, e.X)
		if endsWithPath(e.X) {
			// A '.' right after a path would become part of the path.
			b.WriteByte(' ')
		}
		b.WriteString("." + e.Sel.Name)
	case *ast.CallExpr:
		if h, ok := e.Fun.(*ast.HTTPExpr); ok && len(h.Args) == 0 {
			// Without its own (), the request would take these
			// arguments as its own.
			writeExpr(b, h)
			b.WriteString(" ()")
		} else {
			writeExpr(b, e.Fun)
		}
		writeArgs(b, e.Args)
	case *ast.HTTPExpr:
		b.WriteString(e.Method.String() + " " + path(e.Path))
		if len(e.Args) > 0 {
			b.WriteByte(' ')
			writeArgs(b, e.Args)
		}
	}
}

func writeArgs(b *strings.Builder, args []*ast.Arg) {
	b.WriteByte('(')
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		if a.Name != nil {
			b.WriteString(a.Name.Name + ": ")
		}
		writeExpr(b, a.Value)
	}
	b.WriteByte(')')
}

// endsWithPath reports whether e prints with a request path at its end.
func endsWithPath(e ast.Expr) bool {
	h, ok := e.(*ast.HTTPExpr)
	return ok && len(h.Args) == 0
}
