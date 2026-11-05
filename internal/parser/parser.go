package parser

import (
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/lexer"
	"github.com/sohretaga/govibe/internal/token"
)

// ParseFile parses the source of one .vibe file. It always returns a file;
// source that could not be parsed becomes *ast.Bad nodes, and the
// diagnostics, sorted by position, say why.
func ParseFile(f *token.File) (*ast.File, []diag.Diagnostic) {
	lx := lexer.Lex(f)
	diags := lx.Diags
	p := newParser(f, lx.Tokens, &diags)
	file := &ast.File{Name: f.Name(), Source: f, Decls: p.parseDecls()}
	for _, c := range lx.Comments {
		file.Comments = append(file.Comments, &ast.Comment{Hash: c.Pos, Text: c.Text})
	}
	diag.Sort(diags)
	return file, diags
}

type parser struct {
	f     *token.File
	toks  []token.Token // ends with EOF
	i     int
	tok   token.Token // toks[i]
	diags *[]diag.Diagnostic

	// quiet is set after a syntax error and cleared at the start of the
	// next line, so that one mistake is reported once.
	quiet bool
}

func newParser(f *token.File, toks []token.Token, diags *[]diag.Diagnostic) *parser {
	return &parser{f: f, toks: toks, tok: toks[0], diags: diags}
}

func (p *parser) next() {
	if p.i < len(p.toks)-1 {
		p.i++
	}
	p.tok = p.toks[p.i]
}

func (p *parser) peek(n int) token.Token {
	return p.toks[min(p.i+n, len(p.toks)-1)]
}

func (p *parser) got(k token.Kind) bool {
	if p.tok.Kind == k {
		p.next()
		return true
	}
	return false
}

func (p *parser) skipNewlines() {
	for p.tok.Kind == token.Newline {
		p.next()
	}
}

// skipLine skips to the end of the line or to the '}' that closes the
// current block. Blocks that open on the way are skipped whole.
func (p *parser) skipLine() {
	depth := 0
	for {
		switch p.tok.Kind {
		case token.EOF:
			return
		case token.Newline:
			if depth == 0 {
				return
			}
		case token.LBrace:
			depth++
		case token.RBrace:
			if depth == 0 {
				return
			}
			depth--
		}
		p.next()
	}
}

// maxDiags bounds the diagnostics of one file. Only diag.Limit are shown;
// the margin leaves room for the parser after the lexer's share.
const maxDiags = 2 * diag.Limit

// full reports whether no more diagnostics are recorded for this file.
func (p *parser) full() bool { return len(*p.diags) >= maxDiags }

// diag builds a diagnostic, or an empty one once the file is full, so that
// a long line full of errors does not compute a snippet per error.
func (p *parser) diag(code diag.Code, start, end token.Pos, args ...any) diag.Diagnostic {
	if p.full() {
		return diag.Diagnostic{}
	}
	return diag.New(p.f, code, start, end, args...)
}

// fail reports a syntax error unless one was already reported on this
// line.
func (p *parser) fail(d diag.Diagnostic) {
	if p.quiet {
		return
	}
	p.quiet = true
	p.note(d)
}

// note reports an error after which parsing continues normally.
func (p *parser) note(d diag.Diagnostic) {
	if !p.full() {
		*p.diags = append(*p.diags, d)
	}
}

// errorExpected reports that what was expected at the current token.
func (p *parser) errorExpected(what string) {
	start, end := span(p.tok)
	p.fail(p.diag(diag.V101, start, end, what, describe(p.tok)))
}

// expectClose consumes the closing token k of owner, reporting it missing
// otherwise. It returns the position of the token.
func (p *parser) expectClose(k token.Kind, owner string) token.Pos {
	pos := p.tok.Pos
	if p.got(k) {
		return pos
	}
	start, end := span(p.tok)
	d := p.diag(diag.V101, start, end, quote(k.String())+" to close "+owner, describe(p.tok))
	if p.tok.Kind == token.EOF && k == token.RBrace {
		text := "}\n"
		if src := p.f.Source(); len(src) > 0 && src[len(src)-1] != '\n' {
			text = "\n}\n"
		}
		d = d.WithFix("Insert '}' at the end of the file", diag.Insert(p.f, p.tok.Pos, text))
	}
	p.fail(d)
	return pos
}

// badFrom skips the rest of the line and returns a Bad node from pos.
func (p *parser) badFrom(pos token.Pos) *ast.Bad {
	p.skipLine()
	return &ast.Bad{From: pos, To: p.tok.Pos}
}

// span returns the range a diagnostic about tok covers. A newline is
// reported at the end of its line.
func span(tok token.Token) (token.Pos, token.Pos) {
	if tok.Kind == token.Newline {
		return tok.Pos, tok.Pos
	}
	return tok.Pos, tok.End
}

// describe names a token for "found ..." in messages.
func describe(tok token.Token) string {
	switch tok.Kind {
	case token.Newline:
		return "end of line"
	case token.EOF:
		return "end of file"
	case token.Ident:
		return "name " + quote(tok.Text)
	case token.TypeIdent:
		return "type name " + quote(tok.Text)
	case token.Int, token.Float:
		return "number " + tok.Text
	case token.String:
		text := tok.Text
		if len(text) > 24 {
			text = text[:20] + "...\""
		}
		return "string " + text
	case token.Path:
		return "path " + tok.Text
	}
	if tok.Kind.IsKeyword() {
		return "keyword " + quote(tok.Text)
	}
	return quote(tok.Text)
}

func quote(s string) string { return "'" + s + "'" }

// Names and naming styles.

// parseName parses a declared name that must use the naming style of want:
// token.Ident for snake_case, token.TypeIdent for PascalCase. A name in
// the wrong style is reported and accepted.
func (p *parser) parseName(want token.Kind, what string) *ast.Ident {
	tok := p.tok
	if tok.Kind != token.Ident && tok.Kind != token.TypeIdent {
		start, end := span(tok)
		found := describe(tok)
		if tok.Kind.IsKeyword() {
			found += ", and keywords cannot be used as names"
		}
		p.fail(p.diag(diag.V101, start, end, what, found))
		return nil
	}
	p.next()
	id := &ast.Ident{NamePos: tok.Pos, Name: tok.Text}
	p.checkCase(id, want, what)
	return id
}

// checkCase reports V108 when id does not use the naming style of want.
func (p *parser) checkCase(id *ast.Ident, want token.Kind, what string) {
	if p.full() {
		return
	}
	style, fixed := "snake_case", toSnake(id.Name)
	if want == token.TypeIdent {
		style, fixed = "PascalCase", toPascal(id.Name)
	}
	if fixed == id.Name || (want == token.Ident && isSnake(id.Name)) || (want == token.TypeIdent && isPascal(id.Name)) {
		return
	}
	d := p.diag(diag.V108, id.Pos(), id.End(), what+" "+quote(id.Name), style, quote(fixed))
	p.note(d.WithFix("Write "+quote(fixed), diag.Replace(p.f, id.Pos(), id.End(), fixed)))
}

// checkExprName reports a name in an expression that matches neither
// naming style.
func (p *parser) checkExprName(id *ast.Ident, kind token.Kind) {
	switch {
	case kind == token.Ident:
		p.checkCase(id, token.Ident, "name")
	case strings.ToUpper(id.Name) == id.Name && !isPascal(id.Name):
		p.checkCase(id, token.Ident, "name")
	default:
		p.checkCase(id, token.TypeIdent, "type name")
	}
}

func isSnake(s string) bool {
	if s == "" || !(isLower(s[0]) || s[0] == '_') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !(isLower(c) || isDigit(c) || c == '_') {
			return false
		}
	}
	return true
}

func isPascal(s string) bool {
	if s == "" || !isUpper(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !(isLower(c) || isUpper(c) || isDigit(c)) {
			return false
		}
	}
	return true
}

// toSnake converts myVar, MAX_SIZE and HTTPServer to my_var, max_size and
// http_server.
func toSnake(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isUpper(c) {
			b.WriteByte(c)
			continue
		}
		if i > 0 && s[i-1] != '_' {
			prev := s[i-1]
			nextLower := i+1 < len(s) && isLower(s[i+1])
			if isLower(prev) || isDigit(prev) || isUpper(prev) && nextLower {
				b.WriteByte('_')
			}
		}
		b.WriteByte(c + 'a' - 'A')
	}
	return b.String()
}

// toPascal converts book_review and Book_Review to BookReview.
func toPascal(s string) string {
	var b strings.Builder
	for _, part := range strings.Split(s, "_") {
		if part == "" {
			continue
		}
		if len(part) > 1 && strings.ToUpper(part) == part {
			part = part[:1] + strings.ToLower(part[1:])
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

func isLower(c byte) bool { return 'a' <= c && c <= 'z' }
func isUpper(c byte) bool { return 'A' <= c && c <= 'Z' }
func isDigit(c byte) bool { return '0' <= c && c <= '9' }
