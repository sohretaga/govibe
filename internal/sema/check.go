package sema

import (
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/token"
)

// Check resolves names and types in the files of a project, which must
// have parsed without errors, and builds the program. The program is
// complete only when no diagnostic is an error.
func Check(files []*ast.File) (*ir.Program, []diag.Diagnostic) {
	c := &checker{
		prog:     &ir.Program{},
		models:   map[string]*model{},
		funcs:    map[string]*function{},
		features: map[string]*feature{},
	}
	c.collect(files)
	c.resolveModels()
	c.resolveAuth()
	c.resolveSignatures()
	c.resolveRoutes()
	c.checkBodies()
	diag.Sort(c.diags)
	return c.prog, c.diags
}

// decl locates a declaration for messages that name another place.
type decl struct {
	file *token.File
	node ast.Node
}

func (d decl) where() string {
	return d.file.Name() + ":" + strconv.Itoa(d.file.Line(d.node.Pos()))
}

type model struct {
	ir *ir.Model
	decl
	ast *ast.ModelDecl
}

type function struct {
	ir *ir.Func
	decl
	fn      *ast.FnDecl
	feature *feature // nil for a top-level function
}

type feature struct {
	ir *ir.Feature
	decl
	ast   *ast.FeatureDecl
	funcs map[string]*function
}

type route struct {
	ir *ir.Route
	decl
	ast  *ast.RouteDecl // nil for generated routes
	desc string         // how messages name the route's origin
	feat *feature
}

type test struct {
	ir *ir.Test
	decl
	ast  *ast.TestDecl
	feat *feature
}

type checker struct {
	prog  *ir.Program
	diags []diag.Diagnostic

	app      *decl
	authDecl *decl
	auth     *model
	models   map[string]*model
	order    []*model // models in declaration order
	funcs    map[string]*function
	features map[string]*feature
	featList []*feature
	routes   []*route
	tests    []*test
	fnList   []*function // every function with a body
	allFuncs []*function // every function, externs included
}

func (c *checker) add(d diag.Diagnostic) { c.diags = append(c.diags, d) }

func (c *checker) diagAt(f *token.File, code diag.Code, n ast.Node, args ...any) diag.Diagnostic {
	return diag.New(f, code, n.Pos(), n.End(), args...)
}

func (c *checker) errorAt(f *token.File, code diag.Code, n ast.Node, args ...any) {
	c.add(c.diagAt(f, code, n, args...))
}

func pos(f *token.File, p token.Pos) ir.Pos {
	at := f.Position(p)
	return ir.Pos{File: f.Name(), Line: at.Line, Col: at.Col}
}

func quote(s string) string { return "'" + s + "'" }

// snake converts a PascalCase name to snake_case: BookReview becomes
// book_review.
func snake(name string) string {
	var b strings.Builder
	for i, r := range name {
		if 'A' <= r && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

var (
	baseTypes = map[string]ir.Kind{
		"Int": ir.Int, "Float": ir.Float, "Text": ir.Text, "Bool": ir.Bool,
		"Time": ir.Time, "Money": ir.Money, "Email": ir.Email, "ID": ir.ID,
	}
	builtins = []string{"now", "len", "str", "login_as"}
)

// collect registers the app, auth, models, features and top-level
// functions of every file and reports duplicates.
func (c *checker) collect(files []*ast.File) {
	fnDecls := map[string]decl{}
	for _, file := range files {
		f := file.Source
		for _, d := range file.Decls {
			switch d := d.(type) {
			case *ast.AppDecl:
				c.collectApp(f, d)
			case *ast.AuthDecl:
				if c.authDecl != nil {
					c.errorAt(f, diag.V504, d, "the first is auth "+c.authDecl.node.(*ast.AuthDecl).Model.Name+" at "+c.authDecl.where())
					continue
				}
				c.authDecl = &decl{f, d}
			case *ast.ModelDecl:
				name := d.Name.Name
				if _, ok := baseTypes[name]; ok || name == "Slug" {
					c.errorAt(f, diag.V209, d.Name, "a model", quote(name), "it is a built-in type")
					continue
				}
				if prev, ok := c.models[name]; ok {
					c.errorAt(f, diag.V207, d.Name, "model "+name, prev.where(), decl{f, d}.where())
					continue
				}
				m := &model{ir: &ir.Model{Name: name, Table: snake(name), Pos: pos(f, d.Pos())}, decl: decl{f, d}, ast: d}
				c.models[name] = m
				c.order = append(c.order, m)
				c.prog.Models = append(c.prog.Models, m.ir)
			case *ast.FeatureDecl:
				name := d.Name.Name
				if prev, ok := c.features[name]; ok {
					c.errorAt(f, diag.V207, d.Name, "feature "+name, prev.where(), decl{f, d}.where())
					continue
				}
				ft := &feature{ir: &ir.Feature{Name: name, Pos: pos(f, d.Pos())}, decl: decl{f, d}, ast: d, funcs: map[string]*function{}}
				c.features[name] = ft
				c.featList = append(c.featList, ft)
				c.prog.Features = append(c.prog.Features, ft.ir)
				for _, item := range d.Items {
					if fn, ok := item.(*ast.FnDecl); ok {
						c.collectFunc(nil, f, fn, fn.Name, ft)
					}
				}
			case *ast.FnDecl:
				c.collectFunc(fnDecls, f, d, d.Name, nil)
			case *ast.ExternDecl:
				c.collectFunc(fnDecls, f, d, d.Name, nil)
			}
		}
	}
	if c.app == nil && len(files) > 0 {
		f := files[0].Source
		c.add(diag.New(f, diag.V506, token.PosAt(0), token.PosAt(0),
			`found none; add one to any file, such as app Library { db "sqlite://library.db" }`))
	}
}

func (c *checker) collectApp(f *token.File, d *ast.AppDecl) {
	if c.app != nil {
		c.errorAt(f, diag.V506, d.Name, "found a second one; the first is at "+c.app.where())
		return
	}
	c.app = &decl{f, d}
	app := &ir.App{Name: d.Name.Name, Snake: snake(d.Name.Name), Pos: pos(f, d.Pos())}
	if d.DB != nil {
		if url, ok := plainString(d.DB.URL); ok {
			app.DB = url
		} else {
			c.errorAt(f, diag.V301, d.DB.URL, "the database URL is a plain string, without {...}")
		}
	}
	c.prog.App = app
}

// collectFunc registers a top-level fn or extern, or with ft set a fn of
// a feature.
func (c *checker) collectFunc(seen map[string]decl, f *token.File, d ast.Node, name *ast.Ident, ft *feature) {
	for _, b := range builtins {
		if name.Name == b {
			c.errorAt(f, diag.V209, name, "a function", quote(name.Name), "it is a built-in function")
			return
		}
	}
	if ft != nil {
		if prev, ok := ft.funcs[name.Name]; ok {
			c.errorAt(f, diag.V204, name, "fn "+quote(name.Name), "feature "+ft.ir.Name+" (first at line "+strconv.Itoa(prev.file.Line(prev.node.Pos()))+")")
			return
		}
	}
	if prev, ok := seen[name.Name]; ok {
		c.errorAt(f, diag.V207, name, "fn "+name.Name, prev.where(), decl{f, d}.where())
		return
	}
	fn := &function{ir: &ir.Func{Name: name.Name, Pos: pos(f, d.Pos())}, decl: decl{f, d}, feature: ft}
	c.allFuncs = append(c.allFuncs, fn)
	switch d := d.(type) {
	case *ast.FnDecl:
		fn.fn = d
		c.fnList = append(c.fnList, fn)
	case *ast.ExternDecl:
		fn.ir.Extern = true
	}
	if ft != nil {
		fn.ir.Feature = ft.ir.Name
		ft.funcs[name.Name] = fn
		ft.ir.Funcs = append(ft.ir.Funcs, fn.ir)
		return
	}
	seen[name.Name] = decl{f, d}
	c.funcs[name.Name] = fn
	c.prog.Funcs = append(c.prog.Funcs, fn.ir)
}

// plainString returns the text of a string literal without
// interpolations.
func plainString(s *ast.StringLit) (string, bool) {
	var b strings.Builder
	for _, p := range s.Parts {
		if p.X != nil {
			return "", false
		}
		b.WriteString(p.Text)
	}
	return b.String(), true
}

// modelNames returns the names of all models, for suggestions.
func (c *checker) modelNames() []string {
	names := make([]string, 0, len(c.order))
	for _, m := range c.order {
		names = append(names, m.ir.Name)
	}
	return names
}

// unknownModel reports a name that is not a model, with a suggestion.
func (c *checker) unknownModel(f *token.File, code diag.Code, id *ast.Ident) {
	d := c.diagAt(f, code, id, quote(id.Name))
	if s, ok := diag.Suggest(id.Name, c.modelNames()); ok {
		d = d.WithFix("Use "+quote(s), diag.Replace(f, id.Pos(), id.End(), s))
	}
	c.add(d)
}
