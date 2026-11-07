package sqlgen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
)

// Use tells how a parameter is bound.
type Use int

// Parameter uses.
const (
	Value  Use = iota // the value itself
	Limit             // a row count, clamped to the default cap
	Offset            // a row offset, at least 0
)

// Param is one numbered parameter of a statement: ?1 is Params[0].
type Param struct {
	Expr ir.Expr
	Use  Use
}

// Plan is a static statement and the values it binds.
type Plan struct {
	SQL    string
	Params []Param
}

// maxRows is the most rows a query returns without limit().
const maxRows = 1000

// Columns returns the columns a SELECT of m reads, in the order the
// generated scanner expects.
func Columns(m *ir.Model) []*ir.Field { return m.Fields }

// PasswordColumn holds the bcrypt hash of an account's password in the
// table of the auth model. It is the last column every statement of that
// model reads and writes, and no GoVibe query or JSON shows it.
const PasswordColumn = "password_hash"

// columnNames returns the columns of m in table order.
func columnNames(m *ir.Model) []string {
	cols := make([]string, 0, len(m.Fields)+1)
	for _, f := range m.Fields {
		cols = append(cols, f.Column)
	}
	if m.Auth {
		cols = append(cols, PasswordColumn)
	}
	return cols
}

// columnList returns the columns of m for SQL, each prefixed with
// prefix, such as "u.".
func columnList(m *ir.Model, prefix ...string) string {
	p := strings.Join(prefix, "")
	cols := columnNames(m)
	for i, c := range cols {
		cols[i] = p + ident(c)
	}
	return strings.Join(cols, ", ")
}

// Select returns the plan of a query: its rows, its first row or its
// count.
func Select(q *ir.QueryExpr) Plan {
	b := &builder{}
	where := b.where(q.Where)
	t := ident(q.Model.Table)
	var sql string
	switch q.Result {
	case ir.Count:
		if q.Limit == nil && q.Offset == nil {
			sql = "SELECT COUNT(*) FROM " + t + where
		} else {
			sql = "SELECT COUNT(*) FROM (SELECT 1 FROM " + t + where + b.window(q, "-1") + ")"
		}
	case ir.First:
		limit := "1"
		if q.Limit != nil {
			limit = "min(" + b.param(q.Limit, Limit) + ", 1)"
		}
		sql = "SELECT " + columnList(q.Model) + " FROM " + t + where + order(q.Orders) + " LIMIT " + limit
		if q.Offset != nil {
			sql += " OFFSET " + b.param(q.Offset, Offset)
		}
	default:
		sql = "SELECT " + columnList(q.Model) + " FROM " + t + where + order(q.Orders) + b.window(q, strconv.Itoa(maxRows))
	}
	return Plan{SQL: sql, Params: b.params}
}

// window returns the LIMIT and OFFSET of q; def is the LIMIT without
// limit().
func (b *builder) window(q *ir.QueryExpr, def string) string {
	limit := def
	if q.Limit != nil {
		limit = b.param(q.Limit, Limit)
	}
	s := " LIMIT " + limit
	if q.Offset != nil {
		s += " OFFSET " + b.param(q.Offset, Offset)
	}
	return s
}

// order returns the ORDER BY clause. The id breaks ties, so the order is
// always the same; ids increase with creation time.
func order(orders []ir.Order) string {
	if len(orders) == 0 {
		return ""
	}
	parts := make([]string, 0, len(orders)+1)
	desc := false
	for _, o := range orders {
		parts = append(parts, ident(o.Field.Column)+direction(o.Desc))
		desc = o.Desc
	}
	if last := orders[len(orders)-1].Field; last.Name != "id" {
		parts = append(parts, "id"+direction(desc))
	}
	return " ORDER BY " + strings.Join(parts, ", ")
}

func direction(desc bool) string {
	if desc {
		return " DESC"
	}
	return ""
}

// List returns the statement of a crud list: newest first, with ?1 as the
// limit and ?2 as the offset.
func List(m *ir.Model) string {
	return "SELECT " + columnList(m) + " FROM " + ident(m.Table) + " ORDER BY created_at DESC, id DESC LIMIT ?1 OFFSET ?2"
}

// ListOwned returns the statement of a private crud list: the records
// whose owner field is ?3, newest first, with ?1 as the limit and ?2 as
// the offset.
func ListOwned(m *ir.Model, owner *ir.Field) string {
	return "SELECT " + columnList(m) + " FROM " + ident(m.Table) + " WHERE " + ident(owner.Column) +
		" = ?3 ORDER BY created_at DESC, id DESC LIMIT ?1 OFFSET ?2"
}

// Find returns the statement that loads the record of m whose field f is
// ?1.
func Find(m *ir.Model, f *ir.Field) string {
	return "SELECT " + columnList(m) + " FROM " + ident(m.Table) + " WHERE " + ident(f.Column) + " = ?1"
}

// Exists returns the statement that finds whether a record of m has ?1 in
// field f.
func Exists(m *ir.Model, f *ir.Field) string {
	return "SELECT 1 FROM " + ident(m.Table) + " WHERE " + ident(f.Column) + " = ?1"
}

// Insert returns the statement that stores a new record of m, with one
// parameter per column in Columns order.
func Insert(m *ir.Model) string {
	params := make([]string, len(columnNames(m)))
	for i := range params {
		params[i] = "?" + strconv.Itoa(i+1)
	}
	return "INSERT INTO " + ident(m.Table) + " (" + columnList(m) + ") VALUES (" + strings.Join(params, ", ") + ")"
}

// Update returns the statement that writes a changed record of m, with
// one parameter per column in Columns order; ?1 is the id. created_at
// never changes.
func Update(m *ir.Model) string {
	var sets []string
	for i, c := range columnNames(m) {
		if c == "id" || c == "created_at" {
			continue
		}
		sets = append(sets, ident(c)+" = ?"+strconv.Itoa(i+1))
	}
	return "UPDATE " + ident(m.Table) + " SET " + strings.Join(sets, ", ") + " WHERE id = ?1"
}

// Delete returns the statement that removes the record of m with id ?1.
func Delete(m *ir.Model) string {
	return "DELETE FROM " + ident(m.Table) + " WHERE id = ?1"
}

// builder collects the parameters of a statement.
type builder struct {
	params []Param
}

// param returns the SQL of a value: a literal for a constant, which a
// limit or offset clamps here, and a new parameter otherwise.
func (b *builder) param(e ir.Expr, use Use) string {
	if k, ok := e.(*ir.Const); ok && k.Value != nil {
		if n, ok := k.Value.(int64); ok {
			switch use {
			case Limit:
				return strconv.FormatInt(min(max(n, 0), maxRows), 10)
			case Offset:
				return strconv.FormatInt(max(n, 0), 10)
			}
		}
		return Literal(k)
	}
	b.params = append(b.params, Param{Expr: e, Use: use})
	return "?" + strconv.Itoa(len(b.params))
}

// where returns the WHERE clause of conditions joined with AND.
func (b *builder) where(conds []ir.Expr) string {
	if len(conds) == 0 {
		return ""
	}
	parts := make([]string, len(conds))
	for i, c := range conds {
		parts[i] = b.cond(c).keep()
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

// cond is a condition that may be dropped: when an optional filter value
// is none, its comparison is removed from the condition, as if it had
// never been written. Dropped is the SQL test for that ("" when the
// condition is never dropped) and Value is the condition otherwise.
type cond struct {
	Dropped, Value string
}

// keep is the condition where a dropped one keeps every row, as in an and
// or at the top level.
func (c cond) keep() string {
	if c.Dropped == "" {
		return c.Value
	}
	return "(" + c.Dropped + " OR " + c.Value + ")"
}

// skip is the condition where a dropped one keeps no row, as in an or.
func (c cond) skip() string {
	if c.Dropped == "" {
		return c.Value
	}
	return "(NOT " + c.Dropped + " AND " + c.Value + ")"
}

func both(x, y cond) string {
	if x.Dropped == "" || y.Dropped == "" {
		return ""
	}
	return "(" + x.Dropped + " AND " + y.Dropped + ")"
}

func (b *builder) cond(e ir.Expr) cond {
	if !hasColumn(e) {
		return cond{Value: b.param(e, Value)}
	}
	switch e := e.(type) {
	case *ir.Column:
		return cond{Value: ident(e.Field.Column)}
	case *ir.Unary:
		x := b.cond(e.X)
		return cond{Dropped: x.Dropped, Value: "NOT " + x.Value}
	case *ir.Binary:
		switch e.Op {
		case "and":
			x, y := b.cond(e.X), b.cond(e.Y)
			return cond{Dropped: both(x, y), Value: "(" + x.keep() + " AND " + y.keep() + ")"}
		case "or":
			x, y := b.cond(e.X), b.cond(e.Y)
			return cond{Dropped: both(x, y), Value: "(" + x.skip() + " OR " + y.skip() + ")"}
		}
		return b.compare(e)
	}
	panic(fmt.Sprintf("sqlgen: condition %T", e))
}

// compare translates a comparison with a column on at least one side.
func (b *builder) compare(e *ir.Binary) cond {
	if isNone(e.X) || isNone(e.Y) {
		col := e.X
		if isNone(e.X) {
			col = e.Y
		}
		test := " IS NULL"
		if e.Op == "!=" {
			test = " IS NOT NULL"
		}
		return cond{Value: b.operand(col) + test}
	}
	x, y := b.operand(e.X), b.operand(e.Y)
	var c cond
	switch e.Op {
	case "contains":
		c.Value = "vibe_contains(" + x + ", " + y + ")"
	case "==", "!=":
		op := map[bool]string{true: " = ", false: " <> "}[e.Op == "=="]
		if nullable(e.X) || nullable(e.Y) {
			op = map[bool]string{true: " IS ", false: " IS NOT "}[e.Op == "=="]
		}
		c.Value = x + op + y
	default:
		c.Value = x + " " + e.Op + " " + y
	}
	if e.SkipIfNone {
		value := y
		if _, ok := e.Y.(*ir.Column); ok {
			value = x
		}
		c.Dropped = value + " IS NULL"
	}
	return c
}

func (b *builder) operand(e ir.Expr) string {
	if c, ok := e.(*ir.Column); ok {
		return ident(c.Field.Column)
	}
	return b.param(e, Value)
}

// nullable reports whether e is a column of an optional field or an
// optional value: a comparison with it must treat NULL as a value.
func nullable(e ir.Expr) bool {
	return e.Type().Kind == ir.Optional
}

func isNone(e ir.Expr) bool {
	k, ok := e.(*ir.Const)
	return ok && k.Value == nil
}

// hasColumn reports whether a condition reads a column of the queried
// model; one that does not is computed in Go and bound as a parameter.
func hasColumn(e ir.Expr) bool {
	switch e := e.(type) {
	case *ir.Column:
		return true
	case *ir.Unary:
		return e.Op == "not" && hasColumn(e.X)
	case *ir.Binary:
		return hasColumn(e.X) || hasColumn(e.Y)
	}
	return false
}
