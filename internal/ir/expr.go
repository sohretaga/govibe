package ir

import (
	"fmt"
	"strconv"
)

// Expr is a typed expression.
type Expr interface {
	Type() *Type
	Position() Pos
}

// Stmt is a statement.
type Stmt interface {
	stmt()
}

// typed is embedded in every expression.
type typed struct {
	T *Type
	P Pos
}

// Type returns the type of the expression.
func (t *typed) Type() *Type { return t.T }

// Position returns where the expression starts.
func (t *typed) Position() Pos { return t.P }

// Var is a local: a parameter or a let.
type Var struct {
	Name string
	Type *Type
	Pos  Pos
}

// Const is a constant: int64 for Int and Money (minor units), float64 for
// Float, string for the text types, bool for Bool and nil for none.
type Const struct {
	typed
	Value any
}

// VarRef reads a local. Inside an if that tested it against none, its type
// is the narrowed type while Var.Type stays optional.
type VarRef struct {
	typed
	Var *Var
}

// Me is the logged-in user.
type Me struct{ typed }

// StatusName is a status name compared with a response status.
type StatusName struct {
	typed
	Name string
}

// Unary is -x or not x.
type Unary struct {
	typed
	Op string
	X  Expr
}

// Binary is x op y. Op is the GoVibe spelling: + - * / % == != < <= > >=
// contains and or. In a query condition, SkipIfNone marks a comparison
// whose value is optional: it is dropped when the value is none.
type Binary struct {
	typed
	Op         string
	X, Y       Expr
	SkipIfNone bool
}

// Interp is a string with interpolations; text parts are Consts.
type Interp struct {
	typed
	Parts []Expr
}

// ListExpr is [a, b].
type ListExpr struct {
	typed
	Elems []Expr
}

// FieldRef reads a field of a model value.
type FieldRef struct {
	typed
	X     Expr
	Field *Field
}

// Column is a field of the queried model inside a query condition or
// order.
type Column struct {
	typed
	Field *Field
}

// Member reads a member of a response or of a dynamic JSON value.
type Member struct {
	typed
	X    Expr
	Name string
}

// Call calls a fn or extern fn. Args follow the parameters; omitted
// defaults are filled in.
type Call struct {
	typed
	Func *Func
	Args []Expr
}

// Builtin calls a built-in: now, len, str, login_as, or a Text method
// lower, upper, trim, starts_with, ends_with with the text as Args[0].
type Builtin struct {
	typed
	Name string
	Args []Expr
}

// QueryResult is what a query returns.
type QueryResult int

// Query results.
const (
	All   QueryResult = iota // [M]
	First                    // M?
	Count                    // Int
)

// Order sorts query results by a field.
type Order struct {
	Field *Field
	Desc  bool
}

// QueryExpr is a query over Model: conditions joined with and, orders,
// limit and offset. Its type is Query M until first() or count().
type QueryExpr struct {
	typed
	Model  *Model
	Where  []Expr
	Orders []Order
	Limit  Expr // nil for the default cap
	Offset Expr
	Result QueryResult
}

// Find loads one record by a unique field and answers not_found when
// there is none.
type Find struct {
	typed
	Model *Model
	Field *Field
	Value Expr
}

// FieldValue is one named value of an add or update.
type FieldValue struct {
	Field *Field
	Value Expr
}

// Add creates a record.
type Add struct {
	typed
	Model  *Model
	Values []FieldValue
}

// Update changes fields of a record and returns it.
type Update struct {
	typed
	X      Expr
	Values []FieldValue
}

// Delete removes a record.
type Delete struct {
	typed
	X Expr
}

// Arg is a named argument of a request.
type Arg struct {
	Name  string
	Value Expr
}

// Request sends a request to the app in a test. Path holds the text of
// the path and the values interpolated into it.
type Request struct {
	typed
	Method string
	Path   []Expr
	Args   []Arg
}

// Block is a list of statements.
type Block struct {
	Stmts []Stmt
}

// Let binds a local.
type Let struct {
	Var   *Var
	Value Expr
}

// If is if cond { } else { }; Else is a *Block, an *If or nil.
type If struct {
	Cond Expr
	Then *Block
	Else Stmt
}

// Return returns from a route or function; Value is nil for a bare
// return.
type Return struct {
	Value Expr
	Pos   Pos
}

// Fail stops a route with a status and message.
type Fail struct {
	Status  string
	Message Expr
	Pos     Pos
}

// Expect checks a condition in a test.
type Expect struct {
	Cond Expr
	Pos  Pos
}

// ExprStmt evaluates an expression for its effect.
type ExprStmt struct {
	X Expr
}

func (*Block) stmt()    {}
func (*Let) stmt()      {}
func (*If) stmt()       {}
func (*Return) stmt()   {}
func (*Fail) stmt()     {}
func (*Expect) stmt()   {}
func (*ExprStmt) stmt() {}

// String returns the constant as GoVibe source writes it; Money prints
// with two decimal places.
func (c *Const) String() string {
	switch v := c.Value.(type) {
	case nil:
		return "none"
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return strconv.Quote(v)
	case int64:
		if c.T.Kind == Money || c.T.Kind == Optional && c.T.Elem.Kind == Money {
			sign := ""
			if v < 0 {
				sign, v = "-", -v
			}
			return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
		}
		return strconv.FormatInt(v, 10)
	}
	return fmt.Sprint(c.Value)
}
