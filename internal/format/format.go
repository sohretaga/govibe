package format

import (
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/parser"
	"github.com/sohretaga/govibe/internal/token"
)

// Source formats the .vibe source src named name. If src does not parse,
// it returns nil and the diagnostics.
func Source(name string, src []byte) ([]byte, []diag.Diagnostic) {
	file, diags := parser.ParseFile(token.NewFile(name, src))
	if diag.HasErrors(diags) {
		return nil, diags
	}
	return File(file), diags
}

// File prints a file that parsed without errors in canonical layout.
func File(file *ast.File) []byte {
	p := &printer{f: file.Source, comments: file.Comments, first: true}
	for i, d := range file.Decls {
		p.forceBlank = i > 0
		p.decl(d)
	}
	p.flush(token.Pos(1 << 62))
	out := p.buf.String()
	if out == "" {
		return nil
	}
	return []byte(out)
}

type printer struct {
	f        *token.File
	comments []*ast.Comment
	next     int // index of the first comment not yet printed
	buf      strings.Builder
	indent   int

	lastLine   int  // source line of the last printed comment or code
	first      bool // nothing printed yet in this block
	forceBlank bool // the next thing starts a new declaration or item
}

// gap writes the blank line, if any, before something that starts on
// source line line.
func (p *printer) gap(line int) {
	if !p.first && (p.forceBlank || line > p.lastLine+1) {
		p.buf.WriteByte('\n')
	}
	p.first, p.forceBlank = false, false
}

func (p *printer) writeIndent() {
	p.buf.WriteString(strings.Repeat("  ", p.indent))
}

// flush prints every comment that starts before pos on its own line.
func (p *printer) flush(pos token.Pos) {
	for p.next < len(p.comments) && p.comments[p.next].Hash < pos {
		c := p.comments[p.next]
		p.next++
		line := p.f.Line(c.Hash)
		p.gap(line)
		p.writeIndent()
		p.buf.WriteString(c.Text)
		p.buf.WriteByte('\n')
		p.lastLine = line
	}
}

// endLine appends the comment that follows end on its source line, if
// any, and ends the output line.
func (p *printer) endLine(end token.Pos) {
	line := p.f.Line(end)
	if p.next < len(p.comments) {
		if c := p.comments[p.next]; c.Hash >= end && p.f.Line(c.Hash) == line {
			p.buf.WriteString(" " + c.Text)
			p.next++
		}
	}
	p.buf.WriteByte('\n')
	p.lastLine = line
}

// line prints a construct that fits on one line. Comments inside it move
// above it.
func (p *printer) line(n ast.Node, text string) {
	start := p.f.Line(n.Pos())
	p.flush(n.Pos())
	if p.next < len(p.comments) && p.comments[p.next].Hash < n.End() {
		p.gap(start)
		p.lastLine = start
		p.flush(n.End())
		start = p.lastLine
	}
	p.gap(start)
	p.writeIndent()
	p.buf.WriteString(text)
	p.endLine(n.End())
}

// hasComments reports whether a comment starts between from and to.
func (p *printer) hasComments(from, to token.Pos) bool {
	for _, c := range p.comments[p.next:] {
		if c.Hash >= to {
			return false
		}
		if c.Hash > from {
			return true
		}
	}
	return false
}

// open prints the header of a block construct and its '{'. Comments
// inside the header move above it.
func (p *printer) open(start, lbrace token.Pos, header string) {
	line := p.f.Line(start)
	p.flush(start)
	if p.next < len(p.comments) && p.comments[p.next].Hash < lbrace {
		p.gap(line)
		p.lastLine = line
		p.flush(lbrace)
	}
	p.gap(line)
	p.writeIndent()
	p.buf.WriteString(header + " {")
	p.endLine(lbrace + 1)
	p.indent++
	p.first = true
}

// close prints the comments left before rbrace and the '}' followed by
// suffix. With more set, the line continues, as with "} else {".
func (p *printer) close(rbrace token.Pos, suffix string, more bool) {
	p.flush(rbrace)
	p.indent--
	p.first = false
	p.writeIndent()
	p.buf.WriteString("}" + suffix)
	if !more {
		p.endLine(rbrace + 1)
	}
}

// empty prints n as header {} when its block holds no comments, and
// reports whether it did.
func (p *printer) empty(n ast.Node, lbrace, rbrace token.Pos, header string) bool {
	if p.hasComments(lbrace, rbrace) {
		return false
	}
	p.line(n, header+" {}")
	return true
}

func (p *printer) decl(d ast.Decl) {
	switch d := d.(type) {
	case *ast.AppDecl:
		header := "app " + d.Name.Name
		if d.DB == nil && p.empty(d, d.Lbrace, d.Rbrace, header) {
			return
		}
		p.open(d.App, d.Lbrace, header)
		if d.DB != nil {
			p.line(d.DB, "db "+expr(d.DB.URL))
		}
		p.close(d.Rbrace, "", false)
	case *ast.AuthDecl:
		p.line(d, "auth "+d.Model.Name)
	case *ast.ModelDecl:
		header := "model " + d.Name.Name
		if len(d.Fields) == 0 && p.empty(d, d.Lbrace, d.Rbrace, header) {
			return
		}
		p.open(d.Model, d.Lbrace, header)
		for _, f := range d.Fields {
			p.line(f, field(f))
		}
		p.close(d.Rbrace, "", false)
	case *ast.FeatureDecl:
		header := "feature " + d.Name.Name
		if len(d.Items) == 0 && p.empty(d, d.Lbrace, d.Rbrace, header) {
			return
		}
		p.open(d.Feature, d.Lbrace, header)
		for i, item := range d.Items {
			p.forceBlank = i > 0
			p.item(item)
		}
		p.close(d.Rbrace, "", false)
	case *ast.FnDecl:
		p.fn(d)
	case *ast.ExternDecl:
		p.line(d, "extern fn "+d.Name.Name+params(d.Params, true)+" -> "+typ(d.Result))
	}
}

func (p *printer) item(item ast.Item) {
	switch d := item.(type) {
	case *ast.CrudDecl:
		p.line(d, crud(d))
	case *ast.RouteDecl:
		header := d.Method.String() + " " + path(d.Path) + params(d.Params, false)
		if d.Value != nil {
			p.line(d, header+" = "+expr(d.Value))
			return
		}
		p.body(d, d.Body, header)
	case *ast.FnDecl:
		p.fn(d)
	case *ast.TestDecl:
		p.body(d, d.Body, "test "+expr(d.Name))
	}
}

func (p *printer) fn(d *ast.FnDecl) {
	p.body(d, d.Body, "fn "+d.Name.Name+params(d.Params, true)+" -> "+typ(d.Result))
}

// body prints a construct that ends with a block of statements.
func (p *printer) body(n ast.Node, b *ast.Block, header string) {
	if len(b.Stmts) == 0 && p.empty(n, b.Lbrace, b.Rbrace, header) {
		return
	}
	p.open(n.Pos(), b.Lbrace, header)
	p.stmts(b)
	p.close(b.Rbrace, "", false)
}

func (p *printer) stmts(b *ast.Block) {
	for _, s := range b.Stmts {
		p.stmt(s)
	}
}

func (p *printer) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.IfStmt:
		p.ifStmt(s)
	default:
		p.line(s, simpleStmt(s))
	}
}

// ifStmt prints an if chain. A guard clause, an if without else whose
// block is one fail or return, stays on one line.
func (p *printer) ifStmt(s *ast.IfStmt) {
	if s.Else == nil && len(s.Then.Stmts) == 1 && !p.hasComments(s.Then.Lbrace, s.Then.Rbrace) {
		switch inner := s.Then.Stmts[0].(type) {
		case *ast.FailStmt, *ast.ReturnStmt:
			p.line(s, "if "+expr(s.Cond)+" { "+simpleStmt(inner)+" }")
			return
		}
	}
	p.open(s.If, s.Then.Lbrace, "if "+expr(s.Cond))
	for {
		p.stmts(s.Then)
		switch els := s.Else.(type) {
		case nil:
			p.close(s.Then.Rbrace, "", false)
			return
		case *ast.IfStmt:
			p.close(s.Then.Rbrace, " else if "+expr(els.Cond)+" {", true)
			p.endLine(els.Then.Lbrace + 1)
			p.indent++
			p.first = true
			s = els
		case *ast.Block:
			p.close(s.Then.Rbrace, " else {", true)
			p.endLine(els.Lbrace + 1)
			p.indent++
			p.first = true
			p.stmts(els)
			p.close(els.Rbrace, "", false)
			return
		}
	}
}

func simpleStmt(s ast.Stmt) string {
	switch s := s.(type) {
	case *ast.LetStmt:
		return "let " + s.Name.Name + " = " + expr(s.Value)
	case *ast.ReturnStmt:
		if s.Value == nil {
			return "return"
		}
		return "return " + expr(s.Value)
	case *ast.FailStmt:
		text := "fail "
		if s.Status != nil {
			text += s.Status.Name + " "
		} else if s.Code != nil {
			text += s.Code.Value + " "
		}
		return text + expr(s.Message)
	case *ast.ExpectStmt:
		return "expect " + expr(s.Cond)
	case *ast.ExprStmt:
		return expr(s.X)
	}
	return ""
}

func field(f *ast.Field) string {
	text := f.Name.Name + ": " + typ(f.Type)
	for _, m := range f.Mods {
		text += " " + m.Kind.String()
		if m.Value != nil {
			text += " " + m.Value.Value
		}
	}
	if f.Default != nil {
		text += " = " + expr(f.Default)
	}
	return text
}

func crud(d *ast.CrudDecl) string {
	text := "crud " + d.Model.Name + " at " + path(d.Path)
	if d.Owner != nil {
		text += " owner " + d.Owner.Name
		if d.Private {
			text += " private"
		}
	}
	if len(d.Only) > 0 {
		ops := make([]string, len(d.Only))
		for i, op := range d.Only {
			ops[i] = op.Name
		}
		text += " only " + strings.Join(ops, ", ")
	}
	return text
}

// params prints a parameter list. Routes leave out an empty list;
// functions always print their parentheses, with no space before them.
func params(ps []*ast.Param, fn bool) string {
	if len(ps) == 0 {
		if fn {
			return "()"
		}
		return ""
	}
	list := make([]string, len(ps))
	for i, prm := range ps {
		list[i] = prm.Name.Name
		if prm.Type != nil {
			list[i] += ": " + typ(prm.Type)
		}
		if prm.Default != nil {
			list[i] += " = " + expr(prm.Default)
		}
	}
	text := "(" + strings.Join(list, ", ") + ")"
	if fn {
		return text
	}
	return " " + text
}

func typ(t ast.Type) string {
	switch t := t.(type) {
	case *ast.NamedType:
		return t.Name.Name
	case *ast.ListType:
		return "[" + typ(t.Elem) + "]"
	case *ast.OptionalType:
		return typ(t.Elem) + "?"
	case *ast.RefType:
		return "Ref " + t.Model.Name
	case *ast.SlugType:
		return "Slug(" + t.Source.Name + ")"
	}
	return ""
}

func path(p *ast.Path) string {
	if len(p.Segments) == 0 {
		return "/"
	}
	var b strings.Builder
	for _, s := range p.Segments {
		b.WriteByte('/')
		if s.X != nil {
			b.WriteString("{" + expr(s.X) + "}")
		} else {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}
