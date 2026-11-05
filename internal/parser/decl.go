package parser

import (
	"slices"
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/token"
)

const (
	declList      = "declaration (app, auth, model, feature, fn or extern)"
	itemList      = "feature item (crud, get, post, patch, delete, fn or test)"
	fieldTypeList = "field type (Int, Float, Text, Bool, Time, Money, Email, ID, Slug(field) or Ref Model)"
	crudOpList    = "crud operation (list, get, create, update or delete)"
)

var (
	baseTypes = []string{"Int", "Float", "Text", "Bool", "Time", "Money", "Email", "ID"}
	crudOps   = []string{"list", "get", "create", "update", "delete"}
)

// isDeclStart reports whether k can only start a top-level declaration.
// Block parsers stop there, assuming a missing '}'.
func isDeclStart(k token.Kind) bool {
	switch k {
	case token.App, token.Auth, token.Model, token.Feature, token.Extern:
		return true
	}
	return false
}

func (p *parser) parseDecls() []ast.Decl {
	var decls []ast.Decl
	for {
		p.skipNewlines()
		if p.tok.Kind == token.EOF {
			return decls
		}
		p.quiet = false
		start := p.i
		decls = append(decls, p.parseDecl())
		if p.i == start {
			p.next()
		}
	}
}

func (p *parser) parseDecl() ast.Decl {
	switch p.tok.Kind {
	case token.App:
		return p.parseApp()
	case token.Auth:
		return p.parseAuth()
	case token.Model:
		return p.parseModel()
	case token.Feature:
		return p.parseFeature()
	case token.Fn:
		return p.parseFn()
	case token.Extern:
		return p.parseExtern()
	}
	from := p.tok.Pos
	start, end := span(p.tok)
	d := p.diag(diag.V101, start, end, declList, describe(p.tok))
	switch k := p.tok.Kind; {
	case k == token.Crud || k == token.Test || k.IsMethod():
		d = p.diag(diag.V101, start, end, declList, describe(p.tok)+"; tests, crud and routes go inside a feature")
	case k == token.RBrace:
		d = p.diag(diag.V101, start, end, declList, "'}' that closes no block")
	case k == token.Ident || k == token.TypeIdent:
		if kw, ok := declAlias(p.tok.Text); ok {
			d = d.WithFix("Use "+quote(kw), diag.Replace(p.f, start, end, kw))
		}
	}
	p.fail(d)
	p.skipLine()
	if p.tok.Kind == token.RBrace {
		p.next()
	}
	return &ast.Bad{From: from, To: p.tok.Pos}
}

func declAlias(word string) (string, bool) {
	switch word {
	case "func", "function", "def":
		return "fn", true
	}
	return diag.Suggest(strings.ToLower(word), []string{"app", "auth", "model", "feature", "fn", "extern"})
}

// expectOpen consumes the '{' that opens owner.
func (p *parser) expectOpen(owner string) bool {
	if p.got(token.LBrace) {
		return true
	}
	p.errorExpected("'{' to open " + owner)
	return false
}

func (p *parser) parseApp() ast.Decl {
	d := &ast.AppDecl{App: p.tok.Pos}
	p.next()
	if d.Name = p.parseName(token.TypeIdent, "app name"); d.Name == nil {
		return p.badFrom(d.App)
	}
	owner := "app " + d.Name.Name
	d.Lbrace = p.tok.Pos
	if !p.expectOpen(owner) {
		return p.badFrom(d.App)
	}
	p.skipNewlines()
	if p.tok.Kind == token.DB {
		s := &ast.DBSetting{DB: p.tok.Pos}
		p.next()
		if s.URL = p.parseStringLit("database URL string after db"); s.URL != nil {
			d.DB = s
		} else {
			p.skipLine()
		}
		p.skipNewlines()
	}
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF && !isDeclStart(p.tok.Kind) {
		p.quiet = false
		p.errorExpected("'db' or '}' in " + owner)
		p.skipLine()
		p.skipNewlines()
	}
	p.quiet = false
	d.Rbrace = p.expectClose(token.RBrace, owner)
	return d
}

func (p *parser) parseAuth() ast.Decl {
	d := &ast.AuthDecl{Auth: p.tok.Pos}
	p.next()
	if d.Model = p.parseName(token.TypeIdent, "model name after auth"); d.Model == nil {
		return p.badFrom(d.Auth)
	}
	return d
}

func (p *parser) parseModel() ast.Decl {
	d := &ast.ModelDecl{Model: p.tok.Pos}
	p.next()
	if d.Name = p.parseName(token.TypeIdent, "model name"); d.Name == nil {
		return p.badFrom(d.Model)
	}
	owner := "model " + d.Name.Name
	d.Lbrace = p.tok.Pos
	if !p.expectOpen(owner) {
		return p.badFrom(d.Model)
	}
	p.skipNewlines()
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF && !isDeclStart(p.tok.Kind) && p.tok.Kind != token.Fn {
		p.quiet = false
		start := p.i
		if f := p.parseField(); f != nil {
			d.Fields = append(d.Fields, f)
			p.endItem("field " + quote(f.Name.Name))
		} else {
			p.endItem("field")
		}
		if p.i == start {
			p.next()
		}
	}
	p.quiet = false
	d.Rbrace = p.expectClose(token.RBrace, owner)
	return d
}

// endItem checks that a field or feature item ends its line, as the
// grammar requires.
func (p *parser) endItem(what string) {
	switch p.tok.Kind {
	case token.Newline:
		p.skipNewlines()
	case token.EOF:
	case token.RBrace:
		d := p.diag(diag.V101, p.tok.Pos, p.tok.End, "end of line after "+what, "'}'")
		p.fail(d.WithFix("Put '}' on its own line", diag.Insert(p.f, p.tok.Pos, "\n")))
	default:
		p.errorExpected("end of line after " + what)
		p.skipLine()
		p.skipNewlines()
	}
}

func (p *parser) parseField() *ast.Field {
	name := p.parseName(token.Ident, "field name")
	if name == nil {
		p.skipLine()
		return nil
	}
	f := &ast.Field{Name: name}
	switch k := p.tok.Kind; {
	case k == token.Colon:
		p.next()
	case k == token.TypeIdent || k == token.Ref || k == token.LBrack:
		start, end := span(p.tok)
		d := p.diag(diag.V101, start, end, "':' after field name "+quote(name.Name), describe(p.tok))
		p.fail(d.WithFix("Insert ':'", diag.Insert(p.f, name.End(), ":")))
	default:
		p.errorExpected("':' after field name " + quote(name.Name))
		p.skipLine()
		return nil
	}
	if f.Type = p.parseFieldType(); f.Type == nil {
		p.skipLine()
		return nil
	}
	for p.tok.Kind == token.Unique || p.tok.Kind == token.Min || p.tok.Kind == token.Max {
		m := &ast.FieldMod{KindPos: p.tok.Pos, Kind: p.tok.Kind}
		p.next()
		if m.Kind != token.Unique {
			if m.Value = p.parseNumber("number after " + quote(m.Kind.String())); m.Value == nil {
				p.skipLine()
				return nil
			}
		}
		f.Mods = append(f.Mods, m)
	}
	if p.got(token.Assign) {
		if f.Default = p.parseLiteral("default value"); f.Default == nil {
			p.skipLine()
			return nil
		}
	}
	return f
}

func (p *parser) parseFieldType() ast.Type {
	var t ast.Type
	switch p.tok.Kind {
	case token.Ref:
		r := &ast.RefType{Ref: p.tok.Pos}
		p.next()
		if r.Model = p.parseName(token.TypeIdent, "model name after Ref"); r.Model == nil {
			return nil
		}
		t = r
	case token.TypeIdent:
		if p.tok.Text == "Slug" {
			return p.parseSlug()
		}
		tok := p.tok
		p.next()
		id := &ast.Ident{NamePos: tok.Pos, Name: tok.Text}
		if !slices.Contains(baseTypes, id.Name) {
			d := p.diag(diag.V101, tok.Pos, tok.End, fieldTypeList, describe(tok))
			if s, ok := diag.Suggest(id.Name, baseTypes); ok {
				d = d.WithFix("Use "+quote(s), diag.Replace(p.f, tok.Pos, tok.End, s))
			}
			p.fail(d.WithFix("Use 'Ref "+id.Name+"'", diag.Insert(p.f, tok.Pos, "Ref ")))
		}
		t = &ast.NamedType{Name: id}
	case token.Ident:
		id := &ast.Ident{NamePos: p.tok.Pos, Name: p.tok.Text}
		p.next()
		p.checkCase(id, token.TypeIdent, "type name")
		t = &ast.NamedType{Name: id}
	case token.LBrack:
		from := p.tok.Pos
		if lt := p.parseType(); lt != nil {
			d := p.diag(diag.V101, from, lt.End(), fieldTypeList, "a list type; lists cannot be stored in a model field in v0.1")
			p.fail(d)
		}
		return nil
	default:
		p.errorExpected(fieldTypeList)
		return nil
	}
	if p.tok.Kind == token.Question {
		t = &ast.OptionalType{Elem: t, Question: p.tok.Pos}
		p.next()
	}
	return t
}

func (p *parser) parseSlug() ast.Type {
	s := &ast.SlugType{Slug: p.tok.Pos}
	p.next()
	if !p.got(token.LParen) {
		p.errorExpected("'(' and the source field after Slug, as in Slug(title)")
		return nil
	}
	if s.Source = p.parseName(token.Ident, "source field of Slug"); s.Source == nil {
		return nil
	}
	s.Rparen = p.tok.Pos
	if !p.got(token.RParen) {
		p.errorExpected("')' to close Slug(" + s.Source.Name)
		return nil
	}
	if p.tok.Kind == token.Question {
		d := p.diag(diag.V101, p.tok.Pos, p.tok.End, "end of the field type", "'?'; a slug is never optional")
		p.fail(d.WithFix("Remove '?'", diag.Replace(p.f, p.tok.Pos, p.tok.End, "")))
		p.next()
	}
	return s
}

// parseType parses the type of a parameter or function result.
func (p *parser) parseType() ast.Type {
	var t ast.Type
	switch p.tok.Kind {
	case token.TypeIdent, token.Ident:
		id := &ast.Ident{NamePos: p.tok.Pos, Name: p.tok.Text}
		p.next()
		p.checkCase(id, token.TypeIdent, "type name")
		t = &ast.NamedType{Name: id}
	case token.LBrack:
		l := &ast.ListType{Lbrack: p.tok.Pos}
		p.next()
		if l.Elem = p.parseType(); l.Elem == nil {
			return nil
		}
		l.Rbrack = p.tok.Pos
		if !p.got(token.RBrack) {
			p.errorExpected("']' to close the list type")
			return nil
		}
		t = l
	default:
		p.errorExpected("type")
		return nil
	}
	if p.tok.Kind == token.Question {
		t = &ast.OptionalType{Elem: t, Question: p.tok.Pos}
		p.next()
	}
	return t
}

func (p *parser) parseFeature() ast.Decl {
	d := &ast.FeatureDecl{Feature: p.tok.Pos}
	p.next()
	if d.Name = p.parseName(token.TypeIdent, "feature name"); d.Name == nil {
		return p.badFrom(d.Feature)
	}
	owner := "feature " + d.Name.Name
	d.Lbrace = p.tok.Pos
	if !p.expectOpen(owner) {
		return p.badFrom(d.Feature)
	}
	p.skipNewlines()
	// An extern inside a feature is a misplaced item, not a missing '}'.
	for p.tok.Kind != token.RBrace && p.tok.Kind != token.EOF && (!isDeclStart(p.tok.Kind) || p.tok.Kind == token.Extern) {
		p.quiet = false
		start := p.i
		item, what := p.parseItem()
		d.Items = append(d.Items, item)
		p.endItem(what)
		if p.i == start {
			p.next()
		}
	}
	p.quiet = false
	d.Rbrace = p.expectClose(token.RBrace, owner)
	return d
}

// parseItem parses a feature item and describes it for messages.
func (p *parser) parseItem() (ast.Item, string) {
	switch p.tok.Kind {
	case token.Crud:
		return p.parseCrud(), "crud"
	case token.Get, token.Post, token.Patch, token.Delete:
		return p.parseRoute(), "route"
	case token.Fn:
		return p.parseFn(), "fn"
	case token.Test:
		return p.parseTest(), "test"
	}
	from := p.tok.Pos
	start, end := span(p.tok)
	d := p.diag(diag.V101, start, end, itemList, describe(p.tok))
	switch k := p.tok.Kind; {
	case k == token.Extern:
		d = p.diag(diag.V101, start, end, itemList, "keyword 'extern'; an extern fn is declared at the top level, outside features")
	case k == token.Ident:
		if p.tok.Text == "put" {
			d = d.WithFix("Use 'patch'", diag.Replace(p.f, start, end, "patch"))
		} else if s, ok := diag.Suggest(p.tok.Text, []string{"crud", "get", "post", "patch", "delete", "fn", "test"}); ok {
			d = d.WithFix("Use "+quote(s), diag.Replace(p.f, start, end, s))
		}
	}
	p.fail(d)
	return p.badFrom(from), "item"
}

func (p *parser) parseCrud() ast.Item {
	d := &ast.CrudDecl{Crud: p.tok.Pos}
	p.next()
	if d.Model = p.parseName(token.TypeIdent, "model name after crud"); d.Model == nil {
		return p.badFrom(d.Crud)
	}
	if !p.got(token.At) {
		start, end := span(p.tok)
		e := p.diag(diag.V101, start, end, "'at' and the path after crud "+d.Model.Name, describe(p.tok))
		if p.tok.Kind == token.Slash {
			e = e.WithFix("Insert 'at'", diag.Insert(p.f, p.tok.Pos, "at "))
		}
		p.fail(e)
		return p.badFrom(d.Crud)
	}
	if d.Path = p.parsePath(true); d.Path == nil {
		return p.badFrom(d.Crud)
	}
	if p.got(token.Owner) {
		if d.Owner = p.parseName(token.Ident, "owner field"); d.Owner == nil {
			return p.badFrom(d.Crud)
		}
		if p.tok.Kind == token.Private {
			d.Private, d.PrivatePos = true, p.tok.Pos
			p.next()
		}
	}
	if p.tok.Kind == token.Private {
		p.errorExpected("'owner <field>' before 'private'")
		return p.badFrom(d.Crud)
	}
	if p.got(token.Only) {
		for {
			op := p.parseCrudOp()
			if op == nil {
				return p.badFrom(d.Crud)
			}
			d.Only = append(d.Only, op)
			if !p.got(token.Comma) {
				break
			}
		}
	}
	return d
}

var crudAliases = map[string]string{
	"read": "get", "show": "get", "index": "list", "all": "list",
	"new": "create", "add": "create", "edit": "update", "remove": "delete", "destroy": "delete",
}

func (p *parser) parseCrudOp() *ast.Ident {
	tok := p.tok
	ok := tok.Kind == token.Get || tok.Kind == token.Delete || tok.Kind == token.Ident && slices.Contains(crudOps, tok.Text)
	if ok {
		p.next()
		return &ast.Ident{NamePos: tok.Pos, Name: tok.Text}
	}
	start, end := span(tok)
	d := p.diag(diag.V101, start, end, crudOpList, describe(tok))
	if tok.Kind == token.Ident || tok.Kind == token.TypeIdent {
		word := strings.ToLower(tok.Text)
		op, ok := crudAliases[word]
		if !ok {
			op, ok = diag.Suggest(word, crudOps)
		}
		if ok {
			d = d.WithFix("Use "+quote(op), diag.Replace(p.f, start, end, op))
		}
	}
	p.fail(d)
	return nil
}

func (p *parser) parseRoute() ast.Item {
	d := &ast.RouteDecl{MethodPos: p.tok.Pos, Method: p.tok.Kind}
	p.next()
	if d.Path = p.parsePath(true); d.Path == nil {
		return p.badFrom(d.MethodPos)
	}
	if p.tok.Kind == token.LParen {
		params, ok := p.parseParams()
		if !ok {
			return p.badFrom(d.MethodPos)
		}
		d.Params = params
	}
	owner := "route " + d.Method.String() + " " + p.f.Text(d.Path.Pos(), d.Path.End())
	switch p.tok.Kind {
	case token.Assign:
		p.next()
		d.Value = p.parseExpr()
	case token.LBrace:
		if d.Body = p.parseBlock(owner); d.Body == nil {
			return p.badFrom(d.MethodPos)
		}
	default:
		p.errorExpected("'=' and an expression, or a '{' block, for " + owner)
		return p.badFrom(d.MethodPos)
	}
	return d
}

// parseParams parses ( [ Param { "," Param } [ "," ] ] ).
func (p *parser) parseParams() ([]*ast.Param, bool) {
	p.next() // (
	var params []*ast.Param
	for p.tok.Kind != token.RParen {
		param := p.parseParam()
		if param == nil {
			return nil, false
		}
		params = append(params, param)
		if !p.got(token.Comma) {
			break
		}
	}
	if !p.got(token.RParen) {
		p.errorExpected("',' or ')' in the parameter list")
		return nil, false
	}
	return params, true
}

func (p *parser) parseParam() *ast.Param {
	name := p.parseName(token.Ident, "parameter name")
	if name == nil {
		return nil
	}
	param := &ast.Param{Name: name}
	switch p.tok.Kind {
	case token.Colon:
		p.next()
		if param.Type = p.parseType(); param.Type == nil {
			return nil
		}
		if p.got(token.Assign) {
			if param.Default = p.parseLiteral("default value"); param.Default == nil {
				return nil
			}
		}
	case token.Assign:
		p.next()
		if param.Default = p.parseLiteral("default value"); param.Default == nil {
			return nil
		}
	default:
		p.errorExpected("':' and a type, or '=' and a default, after parameter " + quote(name.Name))
		return nil
	}
	return param
}

// parseSignature parses the name, parameters and result of fn and
// extern fn.
func (p *parser) parseSignature() (name *ast.Ident, params []*ast.Param, result ast.Type, ok bool) {
	if name = p.parseName(token.Ident, "function name"); name == nil {
		return nil, nil, nil, false
	}
	if p.tok.Kind != token.LParen {
		p.errorExpected("'(' and the parameters of fn " + name.Name)
		return nil, nil, nil, false
	}
	if params, ok = p.parseParams(); !ok {
		return nil, nil, nil, false
	}
	switch p.tok.Kind {
	case token.Arrow:
		p.next()
	case token.TypeIdent, token.LBrack:
		start, end := span(p.tok)
		d := p.diag(diag.V101, start, end, "'->' before the result type of fn "+name.Name, describe(p.tok))
		p.fail(d.WithFix("Insert '->'", diag.Insert(p.f, p.tok.Pos, "-> ")))
	default:
		p.errorExpected("'->' and the result type of fn " + name.Name)
		return nil, nil, nil, false
	}
	if result = p.parseType(); result == nil {
		return nil, nil, nil, false
	}
	return name, params, result, true
}

// declItem is a node that is both a declaration and a feature item: a
// function, which may sit at the top level or inside a feature.
type declItem interface {
	ast.Decl
	ast.Item
}

func (p *parser) parseFn() declItem {
	d := &ast.FnDecl{Fn: p.tok.Pos}
	p.next()
	var ok bool
	if d.Name, d.Params, d.Result, ok = p.parseSignature(); !ok {
		return p.badFrom(d.Fn)
	}
	if d.Body = p.parseBlock("fn " + d.Name.Name); d.Body == nil {
		return p.badFrom(d.Fn)
	}
	return d
}

func (p *parser) parseExtern() ast.Decl {
	d := &ast.ExternDecl{Extern: p.tok.Pos}
	p.next()
	if !p.got(token.Fn) {
		p.errorExpected("'fn' after extern")
		return p.badFrom(d.Extern)
	}
	var ok bool
	if d.Name, d.Params, d.Result, ok = p.parseSignature(); !ok {
		return p.badFrom(d.Extern)
	}
	if p.tok.Kind == token.LBrace {
		p.fail(p.diag(diag.V101, p.tok.Pos, p.tok.End, "end of line", "'{'; an extern fn has no body, its Go function lives in the go/ folder"))
		p.skipLine()
	}
	return d
}

func (p *parser) parseTest() ast.Item {
	d := &ast.TestDecl{Test: p.tok.Pos}
	p.next()
	if d.Name = p.parseStringLit("test name string"); d.Name == nil {
		return p.badFrom(d.Test)
	}
	if d.Body = p.parseBlock("test " + p.f.Text(d.Name.Pos(), d.Name.End())); d.Body == nil {
		return p.badFrom(d.Test)
	}
	return d
}
