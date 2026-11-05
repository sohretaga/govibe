package parser

import (
	"slices"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/token"
)

// failStatuses are the status names a fail statement may use.
var failStatuses = []string{"invalid", "login_required", "forbidden", "not_found", "conflict"}

// blockEnds reports whether k cannot appear in a block, so a block that
// meets it is missing its '}'.
func blockEnds(k token.Kind) bool {
	return isDeclStart(k) || k == token.Fn || k == token.Test || k == token.Crud
}

// parseBlock parses a braced block; owner names it in messages.
func (p *parser) parseBlock(owner string) *ast.Block {
	if p.tok.Kind != token.LBrace {
		p.errorExpected("'{' to open " + owner)
		return nil
	}
	b := &ast.Block{Lbrace: p.tok.Pos}
	p.next()
	p.skipNewlines()
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF && !blockEnds(p.tok.Kind) {
		p.quiet = false
		start := p.i
		if p.tok.Kind == token.Else {
			p.misplacedElse(b)
		} else {
			b.Stmts = append(b.Stmts, p.parseStmt())
		}
		p.endStmt()
		if p.i == start {
			p.next()
		}
	}
	p.quiet = false
	b.Rbrace = p.expectClose(token.RBrace, owner)
	return b
}

func (p *parser) endStmt() {
	switch p.tok.Kind {
	case token.Newline:
		p.skipNewlines()
	case token.RBrace, token.EOF:
	default:
		p.errorExpected("end of line after the statement")
		p.skipLine()
		p.skipNewlines()
	}
}

// misplacedElse handles an else at the start of a line. When it follows
// an if, the else is reported with a fix that joins the lines, and then
// attached to the if so that the rest of the block parses normally.
func (p *parser) misplacedElse(b *ast.Block) {
	var last *ast.IfStmt
	if n := len(b.Stmts); n > 0 {
		last, _ = b.Stmts[n-1].(*ast.IfStmt)
	}
	for last != nil {
		next, ok := last.Else.(*ast.IfStmt)
		if !ok {
			break
		}
		last = next
	}
	if last == nil || last.Else != nil {
		p.errorExpected("statement")
		p.skipLine()
		return
	}
	d := p.diag(diag.V101, p.tok.Pos, p.tok.End, "statement", "keyword 'else' at the start of a line; else goes on the same line as the '}' before it")
	p.fail(d.WithFix("Move else after '}'", diag.Replace(p.f, last.Then.End(), p.tok.Pos, " ")))
	p.next()
	if els := p.parseElse(); els != nil {
		last.Else = els
	}
}

func startsExpr(k token.Kind) bool {
	switch k {
	case token.Ident, token.TypeIdent, token.Int, token.Float, token.String,
		token.True, token.False, token.None, token.Me,
		token.LParen, token.LBrack, token.Minus, token.Not,
		token.Get, token.Post, token.Patch, token.Delete:
		return true
	}
	return false
}

func (p *parser) parseStmt() ast.Stmt {
	switch p.tok.Kind {
	case token.Let:
		return p.parseLet()
	case token.If:
		return p.parseIf()
	case token.Return:
		s := &ast.ReturnStmt{Return: p.tok.Pos}
		p.next()
		if k := p.tok.Kind; k != token.Newline && k != token.RBrace && k != token.EOF {
			s.Value = p.parseExpr()
		}
		return s
	case token.Fail:
		return p.parseFail()
	case token.Expect:
		s := &ast.ExpectStmt{Expect: p.tok.Pos}
		p.next()
		s.Cond = p.parseExpr()
		return s
	}
	if !startsExpr(p.tok.Kind) {
		from := p.tok.Pos
		p.errorExpected("statement")
		return p.badFrom(from)
	}
	return &ast.ExprStmt{X: p.parseExpr()}
}

func (p *parser) parseLet() ast.Stmt {
	s := &ast.LetStmt{Let: p.tok.Pos}
	p.next()
	if s.Name = p.parseName(token.Ident, "variable name after let"); s.Name == nil {
		return p.badFrom(s.Let)
	}
	switch p.tok.Kind {
	case token.Assign:
		p.next()
	case token.Eq:
		d := p.diag(diag.V101, p.tok.Pos, p.tok.End, "'=' after "+quote(s.Name.Name), "'=='")
		p.fail(d.WithFix("Use '='", diag.Replace(p.f, p.tok.Pos, p.tok.End, "=")))
		p.next()
	case token.Colon:
		p.fail(p.diag(diag.V101, p.tok.Pos, p.tok.End, "'=' after "+quote(s.Name.Name), "':'; let infers the type from the value"))
		return p.badFrom(s.Let)
	default:
		p.errorExpected("'=' after " + quote(s.Name.Name))
		return p.badFrom(s.Let)
	}
	s.Value = p.parseExpr()
	return s
}

func (p *parser) parseIf() ast.Stmt {
	s := &ast.IfStmt{If: p.tok.Pos}
	p.next()
	s.Cond = p.parseExpr()
	if p.tok.Kind == token.Assign {
		op := p.tok
		d := p.diag(diag.V101, op.Pos, op.End, "'{' after the if condition", "'='; compare with '=='")
		p.fail(d.WithFix("Use '=='", diag.Replace(p.f, op.Pos, op.End, "==")))
		p.next()
		s.Cond = &ast.BinaryExpr{X: s.Cond, OpPos: op.Pos, Op: token.Eq, Y: p.parseBinary(precCompare)}
	}
	if s.Then = p.parseBlock("the if block"); s.Then == nil {
		return p.badFrom(s.If)
	}
	if p.got(token.Else) {
		if els := p.parseElse(); els != nil {
			s.Else = els
		}
	}
	return s
}

// parseElse parses what follows else: a block or another if.
func (p *parser) parseElse() ast.Stmt {
	switch p.tok.Kind {
	case token.If:
		return p.parseIf()
	case token.LBrace:
		if b := p.parseBlock("the else block"); b != nil {
			return b
		}
		return nil
	}
	p.errorExpected("'{' or 'if' after else")
	return nil
}

func (p *parser) parseFail() ast.Stmt {
	s := &ast.FailStmt{Fail: p.tok.Pos}
	p.next()
	switch p.tok.Kind {
	case token.Ident, token.TypeIdent:
		if p.peek(1).Kind != token.String {
			p.errorExpected("status name or message string after fail")
			return p.badFrom(s.Fail)
		}
		s.Status = &ast.Ident{NamePos: p.tok.Pos, Name: p.tok.Text}
		p.checkStatus(p.tok)
		p.next()
	case token.Int:
		// A number instead of a status name; sema reports it with the
		// matching name as a fix.
		s.Code = &ast.BasicLit{ValuePos: p.tok.Pos, Kind: token.Int, Value: p.tok.Text}
		p.next()
	}
	if s.Message = p.parseStringLit("message string after fail"); s.Message == nil {
		return p.badFrom(s.Fail)
	}
	return s
}

func (p *parser) checkStatus(tok token.Token) {
	if slices.Contains(failStatuses, tok.Text) || p.full() {
		return
	}
	d := p.diag(diag.V101, tok.Pos, tok.End, "status name (invalid, login_required, forbidden, not_found or conflict)", describe(tok))
	if s, ok := diag.Suggest(tok.Text, failStatuses); ok {
		d = d.WithFix("Use "+quote(s), diag.Replace(p.f, tok.Pos, tok.End, s))
	}
	p.note(d)
}
