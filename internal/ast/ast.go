package ast

import "github.com/sohretaga/govibe/internal/token"

// Node is a syntax tree node.
type Node interface {
	Pos() token.Pos // first byte of the node
	End() token.Pos // first byte after the node
}

// Decl is a top-level declaration.
type Decl interface {
	Node
	declNode()
}

// Item is a declaration inside a feature block.
type Item interface {
	Node
	itemNode()
}

// Stmt is a statement in a block.
type Stmt interface {
	Node
	stmtNode()
}

// Expr is an expression.
type Expr interface {
	Node
	exprNode()
}

// Type is a type expression.
type Type interface {
	Node
	typeNode()
}

// File is one parsed .vibe file.
type File struct {
	Name     string // slash-separated path relative to the project root
	Decls    []Decl
	Comments []*Comment  // every comment, in source order
	Source   *token.File // the source text the positions refer to
}

// Comment is a comment from '#' to the end of its line.
type Comment struct {
	Hash token.Pos
	Text string // including '#', without trailing spaces
}

// AppDecl is an app block: app Name { db "url" }.
type AppDecl struct {
	App    token.Pos
	Name   *Ident
	Lbrace token.Pos
	DB     *DBSetting // nil without a db line
	Rbrace token.Pos
}

// DBSetting is the db line of an app block.
type DBSetting struct {
	DB  token.Pos
	URL *StringLit
}

// AuthDecl is auth Model.
type AuthDecl struct {
	Auth  token.Pos
	Model *Ident
}

// ModelDecl is model Name { fields }.
type ModelDecl struct {
	Model  token.Pos
	Name   *Ident
	Lbrace token.Pos
	Fields []*Field
	Rbrace token.Pos
}

// Field is a model field: name: Type modifiers = default.
type Field struct {
	Name    *Ident
	Type    Type
	Mods    []*FieldMod
	Default Expr // a literal, or nil
}

// FieldMod is a field modifier: unique, min N or max N.
type FieldMod struct {
	KindPos token.Pos
	Kind    token.Kind // token.Unique, token.Min or token.Max
	Value   *BasicLit  // the number of min and max
}

// FeatureDecl is feature Name { items }.
type FeatureDecl struct {
	Feature token.Pos
	Name    *Ident
	Lbrace  token.Pos
	Items   []Item
	Rbrace  token.Pos
}

// CrudDecl is crud Model at /path [owner field [private]] [only ops].
type CrudDecl struct {
	Crud       token.Pos
	Model      *Ident
	Path       *Path
	Owner      *Ident // nil without owner
	Private    bool
	PrivatePos token.Pos
	Only       []*Ident // the operations after only, if any
}

// RouteDecl is method /path (params) = expr, or the same with a block body.
type RouteDecl struct {
	MethodPos token.Pos
	Method    token.Kind // token.Get, token.Post, token.Patch or token.Delete
	Path      *Path
	Params    []*Param
	Value     Expr   // the = expr form, or nil
	Body      *Block // the block form, or nil
}

// Param is a route or function parameter: name: Type = default, or
// name = default.
type Param struct {
	Name    *Ident
	Type    Type // nil when the default gives the type
	Default Expr // a literal, or nil
}

// FnDecl is fn name(params) -> Type { body }.
type FnDecl struct {
	Fn     token.Pos
	Name   *Ident
	Params []*Param
	Result Type
	Body   *Block
}

// ExternDecl is extern fn name(params) -> Type, implemented in Go.
type ExternDecl struct {
	Extern token.Pos
	Name   *Ident
	Params []*Param
	Result Type
}

// TestDecl is test "name" { body }.
type TestDecl struct {
	Test token.Pos
	Name *StringLit
	Body *Block
}

// Block is a braced list of statements.
type Block struct {
	Lbrace token.Pos
	Stmts  []Stmt
	Rbrace token.Pos
}

// LetStmt is let name = value.
type LetStmt struct {
	Let   token.Pos
	Name  *Ident
	Value Expr
}

// IfStmt is if cond { } with an optional else block or else-if.
type IfStmt struct {
	If   token.Pos
	Cond Expr
	Then *Block
	Else Stmt // *Block, *IfStmt or nil
}

// ReturnStmt is return with an optional value.
type ReturnStmt struct {
	Return token.Pos
	Value  Expr // nil for a bare return
}

// FailStmt is fail [status] "message".
type FailStmt struct {
	Fail    token.Pos
	Status  *Ident    // status name, or nil for the default invalid
	Code    *BasicLit // a number written instead of a status name
	Message *StringLit
}

// ExpectStmt is expect cond, legal only in tests.
type ExpectStmt struct {
	Expect token.Pos
	Cond   Expr
}

// ExprStmt is an expression used as a statement.
type ExprStmt struct {
	X Expr
}

// Ident is a name: snake_case for values, PascalCase for types.
type Ident struct {
	NamePos token.Pos
	Name    string
}

// BasicLit is a number, true, false or none, kept as written.
type BasicLit struct {
	ValuePos token.Pos
	Kind     token.Kind // token.Int, token.Float, token.True, token.False or token.None
	Value    string
}

// StringLit is a string literal, split into text and interpolations.
type StringLit struct {
	Quote  token.Pos
	Parts  []*StringPart
	EndPos token.Pos
}

// StringPart is decoded text or an interpolated expression.
type StringPart struct {
	Text string
	X    Expr // the {expr}, or nil for text
}

// ListLit is [a, b, c].
type ListLit struct {
	Lbrack token.Pos
	Elems  []Expr
	Rbrack token.Pos
}

// ParenExpr is (x).
type ParenExpr struct {
	Lparen token.Pos
	X      Expr
	Rparen token.Pos
}

// UnaryExpr is -x or not x.
type UnaryExpr struct {
	OpPos token.Pos
	Op    token.Kind
	X     Expr
}

// BinaryExpr is x op y.
type BinaryExpr struct {
	X     Expr
	OpPos token.Pos
	Op    token.Kind
	Y     Expr
}

// SelectorExpr is x.name.
type SelectorExpr struct {
	X   Expr
	Sel *Ident
}

// CallExpr is fun(args).
type CallExpr struct {
	Fun    Expr
	Lparen token.Pos
	Args   []*Arg
	Rparen token.Pos
}

// Arg is a call argument with an optional name.
type Arg struct {
	Name  *Ident // nil for a positional argument
	Value Expr
}

// HTTPExpr is a request in a test: method /path (args).
type HTTPExpr struct {
	MethodPos token.Pos
	Method    token.Kind
	Path      *Path
	Args      []*Arg
	Rparen    token.Pos // NoPos without arguments
}

// MeExpr is the logged-in user, me.
type MeExpr struct {
	Me token.Pos
}

// Path is a route path such as /books/{id}. The root path / has no
// segments.
type Path struct {
	Slash    token.Pos
	Segments []*PathSegment
	EndPos   token.Pos
}

// PathSegment is literal text or a {param}. In declarations X is the
// parameter name; in tests it is any expression.
type PathSegment struct {
	SegPos token.Pos
	Text   string
	X      Expr
	EndPos token.Pos
}

// NamedType is a built-in type or a model name.
type NamedType struct {
	Name *Ident
}

// ListType is [T].
type ListType struct {
	Lbrack token.Pos
	Elem   Type
	Rbrack token.Pos
}

// OptionalType is T?.
type OptionalType struct {
	Elem     Type
	Question token.Pos
}

// RefType is Ref Model, valid in model fields.
type RefType struct {
	Ref   token.Pos
	Model *Ident
}

// SlugType is Slug(field), valid in model fields.
type SlugType struct {
	Slug   token.Pos
	Source *Ident
	Rparen token.Pos
}

// Bad stands for source that failed to parse.
type Bad struct {
	From, To token.Pos
}

func (*AppDecl) declNode()     {}
func (*AuthDecl) declNode()    {}
func (*ModelDecl) declNode()   {}
func (*FeatureDecl) declNode() {}
func (*FnDecl) declNode()      {}
func (*ExternDecl) declNode()  {}
func (*Bad) declNode()         {}

func (*CrudDecl) itemNode()  {}
func (*RouteDecl) itemNode() {}
func (*FnDecl) itemNode()    {}
func (*TestDecl) itemNode()  {}
func (*Bad) itemNode()       {}

func (*Block) stmtNode()      {}
func (*LetStmt) stmtNode()    {}
func (*IfStmt) stmtNode()     {}
func (*ReturnStmt) stmtNode() {}
func (*FailStmt) stmtNode()   {}
func (*ExpectStmt) stmtNode() {}
func (*ExprStmt) stmtNode()   {}
func (*Bad) stmtNode()        {}

func (*Ident) exprNode()        {}
func (*BasicLit) exprNode()     {}
func (*StringLit) exprNode()    {}
func (*ListLit) exprNode()      {}
func (*ParenExpr) exprNode()    {}
func (*UnaryExpr) exprNode()    {}
func (*BinaryExpr) exprNode()   {}
func (*SelectorExpr) exprNode() {}
func (*CallExpr) exprNode()     {}
func (*HTTPExpr) exprNode()     {}
func (*MeExpr) exprNode()       {}
func (*Bad) exprNode()          {}

func (*NamedType) typeNode()    {}
func (*ListType) typeNode()     {}
func (*OptionalType) typeNode() {}
func (*RefType) typeNode()      {}
func (*SlugType) typeNode()     {}
func (*Bad) typeNode()          {}

func after(p token.Pos, s string) token.Pos { return p + token.Pos(len(s)) }

// Pos and End implementations.

func (d *AppDecl) Pos() token.Pos     { return d.App }
func (d *AppDecl) End() token.Pos     { return d.Rbrace + 1 }
func (d *DBSetting) Pos() token.Pos   { return d.DB }
func (d *DBSetting) End() token.Pos   { return d.URL.End() }
func (d *AuthDecl) Pos() token.Pos    { return d.Auth }
func (d *AuthDecl) End() token.Pos    { return d.Model.End() }
func (d *ModelDecl) Pos() token.Pos   { return d.Model }
func (d *ModelDecl) End() token.Pos   { return d.Rbrace + 1 }
func (f *Field) Pos() token.Pos       { return f.Name.Pos() }
func (m *FieldMod) Pos() token.Pos    { return m.KindPos }
func (d *FeatureDecl) Pos() token.Pos { return d.Feature }
func (d *FeatureDecl) End() token.Pos { return d.Rbrace + 1 }
func (d *CrudDecl) Pos() token.Pos    { return d.Crud }
func (d *RouteDecl) Pos() token.Pos   { return d.MethodPos }
func (p *Param) Pos() token.Pos       { return p.Name.Pos() }
func (d *FnDecl) Pos() token.Pos      { return d.Fn }
func (d *FnDecl) End() token.Pos      { return d.Body.End() }
func (d *ExternDecl) Pos() token.Pos  { return d.Extern }
func (d *ExternDecl) End() token.Pos  { return d.Result.End() }
func (d *TestDecl) Pos() token.Pos    { return d.Test }
func (d *TestDecl) End() token.Pos    { return d.Body.End() }

func (b *Block) Pos() token.Pos        { return b.Lbrace }
func (b *Block) End() token.Pos        { return b.Rbrace + 1 }
func (s *LetStmt) Pos() token.Pos      { return s.Let }
func (s *LetStmt) End() token.Pos      { return s.Value.End() }
func (s *IfStmt) Pos() token.Pos       { return s.If }
func (s *ReturnStmt) Pos() token.Pos   { return s.Return }
func (s *FailStmt) Pos() token.Pos     { return s.Fail }
func (s *FailStmt) End() token.Pos     { return s.Message.End() }
func (s *ExpectStmt) Pos() token.Pos   { return s.Expect }
func (s *ExpectStmt) End() token.Pos   { return s.Cond.End() }
func (s *ExprStmt) Pos() token.Pos     { return s.X.Pos() }
func (s *ExprStmt) End() token.Pos     { return s.X.End() }
func (x *Ident) Pos() token.Pos        { return x.NamePos }
func (x *Ident) End() token.Pos        { return after(x.NamePos, x.Name) }
func (x *BasicLit) Pos() token.Pos     { return x.ValuePos }
func (x *BasicLit) End() token.Pos     { return after(x.ValuePos, x.Value) }
func (x *StringLit) Pos() token.Pos    { return x.Quote }
func (x *StringLit) End() token.Pos    { return x.EndPos }
func (x *ListLit) Pos() token.Pos      { return x.Lbrack }
func (x *ListLit) End() token.Pos      { return x.Rbrack + 1 }
func (x *ParenExpr) Pos() token.Pos    { return x.Lparen }
func (x *ParenExpr) End() token.Pos    { return x.Rparen + 1 }
func (x *UnaryExpr) Pos() token.Pos    { return x.OpPos }
func (x *UnaryExpr) End() token.Pos    { return x.X.End() }
func (x *BinaryExpr) Pos() token.Pos   { return x.X.Pos() }
func (x *BinaryExpr) End() token.Pos   { return x.Y.End() }
func (x *SelectorExpr) Pos() token.Pos { return x.X.Pos() }
func (x *SelectorExpr) End() token.Pos { return x.Sel.End() }
func (x *CallExpr) Pos() token.Pos     { return x.Fun.Pos() }
func (x *CallExpr) End() token.Pos     { return x.Rparen + 1 }
func (a *Arg) End() token.Pos          { return a.Value.End() }
func (x *HTTPExpr) Pos() token.Pos     { return x.MethodPos }
func (x *MeExpr) Pos() token.Pos       { return x.Me }
func (x *MeExpr) End() token.Pos       { return after(x.Me, "me") }
func (p *Path) Pos() token.Pos         { return p.Slash }
func (p *Path) End() token.Pos         { return p.EndPos }
func (s *PathSegment) Pos() token.Pos  { return s.SegPos }
func (s *PathSegment) End() token.Pos  { return s.EndPos }
func (t *NamedType) Pos() token.Pos    { return t.Name.Pos() }
func (t *NamedType) End() token.Pos    { return t.Name.End() }
func (t *ListType) Pos() token.Pos     { return t.Lbrack }
func (t *ListType) End() token.Pos     { return t.Rbrack + 1 }
func (t *OptionalType) Pos() token.Pos { return t.Elem.Pos() }
func (t *OptionalType) End() token.Pos { return t.Question + 1 }
func (t *RefType) Pos() token.Pos      { return t.Ref }
func (t *RefType) End() token.Pos      { return t.Model.End() }
func (t *SlugType) Pos() token.Pos     { return t.Slug }
func (t *SlugType) End() token.Pos     { return t.Rparen + 1 }
func (b *Bad) Pos() token.Pos          { return b.From }
func (b *Bad) End() token.Pos          { return b.To }

// End returns the end of the field's last part.
func (f *Field) End() token.Pos {
	switch {
	case f.Default != nil:
		return f.Default.End()
	case len(f.Mods) > 0:
		return f.Mods[len(f.Mods)-1].End()
	}
	return f.Type.End()
}

// End returns the end of the modifier and its number.
func (m *FieldMod) End() token.Pos {
	if m.Value != nil {
		return m.Value.End()
	}
	return after(m.KindPos, m.Kind.String())
}

// End returns the end of the crud declaration's last clause.
func (d *CrudDecl) End() token.Pos {
	switch {
	case len(d.Only) > 0:
		return d.Only[len(d.Only)-1].End()
	case d.Private:
		return after(d.PrivatePos, "private")
	case d.Owner != nil:
		return d.Owner.End()
	}
	return d.Path.End()
}

// End returns the end of the route's value or body.
func (d *RouteDecl) End() token.Pos {
	if d.Body != nil {
		return d.Body.End()
	}
	return d.Value.End()
}

// End returns the end of the parameter's last part.
func (p *Param) End() token.Pos {
	if p.Default != nil {
		return p.Default.End()
	}
	return p.Type.End()
}

// End returns the end of the last block of the if chain.
func (s *IfStmt) End() token.Pos {
	if s.Else != nil {
		return s.Else.End()
	}
	return s.Then.End()
}

// End returns the end of the returned value, or of the keyword.
func (s *ReturnStmt) End() token.Pos {
	if s.Value != nil {
		return s.Value.End()
	}
	return after(s.Return, "return")
}

// Pos returns the start of the argument's name or value.
func (a *Arg) Pos() token.Pos {
	if a.Name != nil {
		return a.Name.Pos()
	}
	return a.Value.Pos()
}

// End returns the end of the request's arguments or path.
func (x *HTTPExpr) End() token.Pos {
	if x.Rparen.IsValid() {
		return x.Rparen + 1
	}
	return x.Path.End()
}
