package ast

// Inspect visits n and its descendants in source order. If f returns false
// for a node, the children of that node are skipped.
func Inspect(n Node, f func(Node) bool) {
	if n == nil || !f(n) {
		return
	}
	switch n := n.(type) {
	case *AppDecl:
		visit(f, n.Name)
		visit(f, n.DB)
	case *DBSetting:
		visit(f, n.URL)
	case *AuthDecl:
		visit(f, n.Model)
	case *ModelDecl:
		visit(f, n.Name)
		visit(f, n.Fields...)
	case *Field:
		visit(f, n.Name)
		visit(f, n.Type)
		visit(f, n.Mods...)
		visit(f, n.Default)
	case *FieldMod:
		visit(f, n.Value)
	case *FeatureDecl:
		visit(f, n.Name)
		visit(f, n.Items...)
	case *CrudDecl:
		visit(f, n.Model)
		visit(f, n.Path)
		visit(f, n.Owner)
		visit(f, n.Only...)
	case *RouteDecl:
		visit(f, n.Path)
		visit(f, n.Params...)
		visit(f, n.Value)
		visit(f, n.Body)
	case *Param:
		visit(f, n.Name)
		visit(f, n.Type)
		visit(f, n.Default)
	case *FnDecl:
		visit(f, n.Name)
		visit(f, n.Params...)
		visit(f, n.Result)
		visit(f, n.Body)
	case *ExternDecl:
		visit(f, n.Name)
		visit(f, n.Params...)
		visit(f, n.Result)
	case *TestDecl:
		visit(f, n.Name)
		visit(f, n.Body)
	case *Block:
		visit(f, n.Stmts...)
	case *LetStmt:
		visit(f, n.Name)
		visit(f, n.Value)
	case *IfStmt:
		visit(f, n.Cond)
		visit(f, n.Then)
		visit(f, n.Else)
	case *ReturnStmt:
		visit(f, n.Value)
	case *FailStmt:
		visit(f, n.Status)
		visit(f, n.Code)
		visit(f, n.Message)
	case *ExpectStmt:
		visit(f, n.Cond)
	case *ExprStmt:
		visit(f, n.X)
	case *StringLit:
		for _, p := range n.Parts {
			visit(f, p.X)
		}
	case *ListLit:
		visit(f, n.Elems...)
	case *ParenExpr:
		visit(f, n.X)
	case *UnaryExpr:
		visit(f, n.X)
	case *BinaryExpr:
		visit(f, n.X)
		visit(f, n.Y)
	case *SelectorExpr:
		visit(f, n.X)
		visit(f, n.Sel)
	case *CallExpr:
		visit(f, n.Fun)
		visit(f, n.Args...)
	case *Arg:
		visit(f, n.Name)
		visit(f, n.Value)
	case *HTTPExpr:
		visit(f, n.Path)
		visit(f, n.Args...)
	case *Path:
		visit(f, n.Segments...)
	case *PathSegment:
		visit(f, n.X)
	case *NamedType:
		visit(f, n.Name)
	case *ListType:
		visit(f, n.Elem)
	case *OptionalType:
		visit(f, n.Elem)
	case *RefType:
		visit(f, n.Model)
	case *SlugType:
		visit(f, n.Source)
	}
}

// visit inspects each node that is not nil. Pointer and interface types
// are both comparable, so the zero value detects either kind of nil.
func visit[T interface {
	Node
	comparable
}](f func(Node) bool, nodes ...T) {
	var zero T
	for _, n := range nodes {
		if n != zero {
			Inspect(n, f)
		}
	}
}

// InspectFile calls Inspect on every declaration of file.
func InspectFile(file *File, f func(Node) bool) {
	for _, d := range file.Decls {
		Inspect(d, f)
	}
}
