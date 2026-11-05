package parser

import (
	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/token"
)

// Binding powers, from loosest to tightest. Prefix not binds looser than
// comparisons, so not a == b is not (a == b).
const (
	precLowest = iota
	precOr
	precAnd
	precNot
	precCompare
	precAdd
	precMul
	precUnary
)

func infixPrec(k token.Kind) int {
	switch {
	case k == token.Or:
		return precOr
	case k == token.And:
		return precAnd
	case k.IsComparison():
		return precCompare
	case k == token.Plus, k == token.Minus:
		return precAdd
	case k == token.Star, k == token.Slash, k == token.Percent:
		return precMul
	}
	return precLowest
}

// parseExpr parses an expression. It never returns nil.
func (p *parser) parseExpr() ast.Expr {
	return p.parseBinary(precLowest)
}

// parseBinary parses operators that bind tighter than min, left to right.
func (p *parser) parseBinary(min int) ast.Expr {
	x := p.parseUnary(min)
	for {
		prec := infixPrec(p.tok.Kind)
		if prec <= min {
			return x
		}
		op := p.tok
		p.next()
		y := p.parseBinary(prec)
		if prec == precCompare {
			if c, ok := x.(*ast.BinaryExpr); ok && c.Op.IsComparison() {
				p.chainedComparison(c, op, y)
			}
		}
		x = &ast.BinaryExpr{X: x, OpPos: op.Pos, Op: op.Kind, Y: y}
	}
}

// chainedComparison reports a < b < c and suggests a < b and b < c.
func (p *parser) chainedComparison(first *ast.BinaryExpr, op token.Token, y ast.Expr) {
	if p.full() {
		return
	}
	whole := p.f.Text(first.Pos(), y.End())
	d := p.diag(diag.V105, first.Pos(), y.End(), quote(whole))
	middle := p.f.Text(first.Y.Pos(), first.Y.End())
	fixed := p.f.Text(first.Pos(), first.End()) + " and " + middle + " " + op.Text + " " + p.f.Text(y.Pos(), y.End())
	p.note(d.WithFix("Use "+quote(fixed), diag.Replace(p.f, first.Pos(), y.End(), fixed)))
}

func (p *parser) parseUnary(min int) ast.Expr {
	switch p.tok.Kind {
	case token.Not:
		pos := p.tok.Pos
		p.next()
		x := &ast.UnaryExpr{OpPos: pos, Op: token.Not, X: p.parseBinary(precNot)}
		if min >= precCompare && !p.full() {
			// The grammar allows not only at the start of an operand of and
			// or or: a == not b needs parentheses.
			d := p.diag(diag.V101, pos, x.End(), "an operand without 'not'; wrap the negation in parentheses", "keyword 'not'")
			p.note(d.WithFix("Add parentheses", diag.Insert(p.f, pos, "("), diag.Insert(p.f, x.End(), ")")))
		}
		return x
	case token.Minus:
		pos := p.tok.Pos
		p.next()
		return &ast.UnaryExpr{OpPos: pos, Op: token.Minus, X: p.parseBinary(precUnary)}
	}
	return p.parsePostfix()
}

func (p *parser) parsePostfix() ast.Expr {
	x := p.parsePrimary()
	for {
		switch p.tok.Kind {
		case token.Dot:
			p.next()
			sel := p.parseMember()
			if sel == nil {
				return x
			}
			x = &ast.SelectorExpr{X: x, Sel: sel}
		case token.LParen:
			callee := p.f.Text(x.Pos(), x.End())
			lparen, args, rparen := p.parseArgs(callee)
			x = &ast.CallExpr{Fun: x, Lparen: lparen, Args: args, Rparen: rparen}
		default:
			return x
		}
	}
}

// parseMember parses the name after '.', where keywords are plain names.
func (p *parser) parseMember() *ast.Ident {
	tok := p.tok
	switch {
	case tok.Kind == token.Ident || tok.Kind.IsKeyword():
		p.next()
		id := &ast.Ident{NamePos: tok.Pos, Name: tok.Text}
		if tok.Kind == token.Ident {
			p.checkCase(id, token.Ident, "member name")
		}
		return id
	case tok.Kind == token.TypeIdent:
		p.next()
		id := &ast.Ident{NamePos: tok.Pos, Name: tok.Text}
		p.checkCase(id, token.Ident, "member name")
		return id
	}
	p.errorExpected("member name after '.'")
	return nil
}

// parseArgs parses ( [ Arg { "," Arg } [ "," ] ) where Arg is
// [ name ":" ] Expr, and reports V107 when named and positional
// arguments are mixed. callee names the call in messages.
func (p *parser) parseArgs(callee string) (lparen token.Pos, args []*ast.Arg, rparen token.Pos) {
	lparen = p.tok.Pos
	p.next()
	for p.tok.Kind != token.RParen && p.tok.Kind != token.EOF {
		arg := &ast.Arg{}
		if (p.tok.Kind == token.Ident || p.tok.Kind == token.TypeIdent) && p.peek(1).Kind == token.Colon {
			arg.Name = p.parseName(token.Ident, "argument name")
			p.next() // :
		}
		arg.Value = p.parseExpr()
		args = append(args, arg)
		if !p.got(token.Comma) {
			break
		}
	}
	rparen = p.expectClose(token.RParen, "the arguments of "+callee)
	if len(args) > 1 && !p.full() {
		named := args[0].Name != nil
		for _, a := range args[1:] {
			if (a.Name != nil) != named {
				p.note(p.diag(diag.V107, a.Pos(), a.End(), quote(callee)))
				break
			}
		}
	}
	return lparen, args, rparen
}

func (p *parser) parsePrimary() ast.Expr {
	tok := p.tok
	switch tok.Kind {
	case token.Ident, token.TypeIdent:
		p.next()
		id := &ast.Ident{NamePos: tok.Pos, Name: tok.Text}
		p.checkExprName(id, tok.Kind)
		return id
	case token.Int, token.Float, token.True, token.False, token.None:
		p.next()
		return &ast.BasicLit{ValuePos: tok.Pos, Kind: tok.Kind, Value: tok.Text}
	case token.String:
		return p.parseStringLit("string")
	case token.Me:
		p.next()
		return &ast.MeExpr{Me: tok.Pos}
	case token.LParen:
		p.next()
		x := &ast.ParenExpr{Lparen: tok.Pos, X: p.parseExpr()}
		x.Rparen = p.expectClose(token.RParen, "'('")
		return x
	case token.LBrack:
		return p.parseList()
	case token.Get, token.Post, token.Patch, token.Delete:
		return p.parseHTTP()
	}
	p.errorExpected("expression")
	return &ast.Bad{From: tok.Pos, To: tok.Pos}
}

func (p *parser) parseList() ast.Expr {
	l := &ast.ListLit{Lbrack: p.tok.Pos}
	p.next()
	for p.tok.Kind != token.RBrack && p.tok.Kind != token.EOF {
		l.Elems = append(l.Elems, p.parseExpr())
		if !p.got(token.Comma) {
			break
		}
	}
	l.Rbrack = p.expectClose(token.RBrack, "the list")
	return l
}

func (p *parser) parseHTTP() ast.Expr {
	x := &ast.HTTPExpr{MethodPos: p.tok.Pos, Method: p.tok.Kind}
	p.next()
	if x.Path = p.parsePath(false); x.Path == nil {
		return &ast.Bad{From: x.MethodPos, To: p.tok.Pos}
	}
	if p.tok.Kind == token.LParen {
		callee := x.Method.String() + " " + p.f.Text(x.Path.Pos(), x.Path.End())
		_, x.Args, x.Rparen = p.parseArgs(callee)
	}
	return x
}

// parsePath converts a Path token. In declarations each {param} must be a
// name; in tests it is any expression.
func (p *parser) parsePath(decl bool) *ast.Path {
	tok := p.tok
	if tok.Kind != token.Path {
		start, end := span(tok)
		d := p.diag(diag.V101, start, end, "path starting with '/'", describe(tok))
		if tok.Kind == token.Ident || tok.Kind == token.TypeIdent || tok.Kind == token.Int {
			d = d.WithFix("Insert '/'", diag.Insert(p.f, tok.Pos, "/"))
		}
		p.fail(d)
		return nil
	}
	p.next()
	path := &ast.Path{Slash: tok.Pos, EndPos: tok.End}
	for _, part := range tok.Parts {
		seg := &ast.PathSegment{SegPos: part.Pos, EndPos: part.End, Text: part.Text}
		if part.Expr {
			seg.SegPos, seg.EndPos = part.Pos-1, part.End+1
			if decl {
				seg.X = p.pathParam(part)
			} else {
				seg.X = p.embedded(part)
			}
		}
		path.Segments = append(path.Segments, seg)
	}
	return path
}

// pathParam parses the {name} of a path in a declaration.
func (p *parser) pathParam(part token.Part) ast.Expr {
	toks := part.Tokens
	if len(toks) == 2 && (toks[0].Kind == token.Ident || toks[0].Kind == token.TypeIdent) {
		id := &ast.Ident{NamePos: toks[0].Pos, Name: toks[0].Text}
		p.checkCase(id, token.Ident, "path parameter")
		return id
	}
	found := "'}'"
	if len(toks) > 1 {
		found = describe(toks[0])
		if len(toks) > 2 {
			found = "an expression"
		}
	}
	p.fail(p.diag(diag.V101, part.Pos-1, part.End+1, "parameter name inside {...} of the path", found))
	return &ast.Bad{From: part.Pos, To: part.End}
}

// embedded parses the {expr} of a string or of a path in a test.
func (p *parser) embedded(part token.Part) ast.Expr {
	if len(part.Tokens) <= 1 {
		p.fail(p.diag(diag.V101, part.Pos-1, part.End+1, "expression inside {...}", "'}'"))
		return &ast.Bad{From: part.Pos, To: part.End}
	}
	sub := newParser(p.f, part.Tokens, p.diags)
	x := sub.parseExpr()
	if sub.tok.Kind != token.EOF {
		sub.errorExpected("'}' after the expression")
	}
	return x
}

// parseStringLit parses a string token, or reports what was expected.
func (p *parser) parseStringLit(what string) *ast.StringLit {
	tok := p.tok
	if tok.Kind != token.String {
		p.errorExpected(what)
		return nil
	}
	p.next()
	s := &ast.StringLit{Quote: tok.Pos, EndPos: tok.End}
	for _, part := range tok.Parts {
		if part.Expr {
			s.Parts = append(s.Parts, &ast.StringPart{X: p.embedded(part)})
		} else {
			s.Parts = append(s.Parts, &ast.StringPart{Text: part.Text})
		}
	}
	return s
}

// parseLiteral parses a number, string, true, false or none. It returns
// nil after reporting anything else.
func (p *parser) parseLiteral(what string) ast.Expr {
	switch tok := p.tok; tok.Kind {
	case token.Int, token.Float, token.True, token.False, token.None:
		p.next()
		return &ast.BasicLit{ValuePos: tok.Pos, Kind: tok.Kind, Value: tok.Text}
	case token.String:
		return p.parseStringLit(what)
	}
	p.errorExpected(what + ": a number, string, true, false or none")
	return nil
}

func (p *parser) parseNumber(what string) *ast.BasicLit {
	tok := p.tok
	if tok.Kind != token.Int && tok.Kind != token.Float {
		p.errorExpected(what)
		return nil
	}
	p.next()
	return &ast.BasicLit{ValuePos: tok.Pos, Kind: tok.Kind, Value: tok.Text}
}
