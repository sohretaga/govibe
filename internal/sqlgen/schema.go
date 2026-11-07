package sqlgen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
)

// Table is the table of a model.
type Table struct {
	Model   *ir.Model
	Name    string
	Create  string // CREATE TABLE statement
	Columns []Column
	Indexes []string // CREATE INDEX IF NOT EXISTS statements
}

// Column is one column of a table.
type Column struct {
	Field   *ir.Field // nil for the password hash and session columns
	Name    string
	Type    string // INTEGER, REAL or TEXT
	NotNull bool
	// Add adds the column to a table that already has rows, or is ""
	// when that is impossible: a required column without a default.
	Add string
}

// Schema returns the tables of every model, in declaration order, and
// with auth the sessions table.
func Schema(p *ir.Program) []Table {
	out := make([]Table, 0, len(p.Models)+1)
	for _, m := range p.Models {
		out = append(out, table(m))
	}
	if p.Auth != nil {
		out = append(out, sessionTable(p.Auth))
	}
	return out
}

func table(m *ir.Model) Table {
	t := Table{Model: m, Name: m.Table}
	defs := make([]string, 0, len(m.Fields))
	for _, f := range m.Fields {
		c := Column{Field: f, Name: f.Column, Type: columnType(f.Type), NotNull: f.Type.Kind != ir.Optional}
		def := definition(f, c)
		defs = append(defs, def)
		if !c.NotNull || f.Default != nil {
			c.Add = "ALTER TABLE " + ident(m.Table) + " ADD COLUMN " + def
		}
		t.Columns = append(t.Columns, c)
		t.Indexes = append(t.Indexes, indexes(m, f)...)
	}
	if m.Auth {
		c := Column{Name: PasswordColumn, Type: "TEXT"}
		def := ident(c.Name) + " TEXT"
		c.Add = "ALTER TABLE " + ident(m.Table) + " ADD COLUMN " + def
		defs = append(defs, def)
		t.Columns = append(t.Columns, c)
	}
	t.Create = "CREATE TABLE " + ident(m.Table) + " (" + strings.Join(defs, ", ") + ") STRICT"
	return t
}

// definition returns the column definition of f, as CREATE TABLE and ADD
// COLUMN take it.
func definition(f *ir.Field, c Column) string {
	var b strings.Builder
	b.WriteString(ident(c.Name) + " " + c.Type)
	if c.NotNull {
		b.WriteString(" NOT NULL")
	}
	if f.Name == "id" && f.Implicit {
		b.WriteString(" PRIMARY KEY")
	}
	if f.Default != nil && f.Default.Value != nil {
		b.WriteString(" DEFAULT " + Literal(f.Default))
	}
	if f.Ref != nil {
		b.WriteString(" REFERENCES " + ident(f.Ref.Table) + " (id)")
	}
	return b.String()
}

// indexes returns the indexes of field f: a unique index for unique
// fields, an index for Ref fields and for created_at, which list and
// newest() sort by.
func indexes(m *ir.Model, f *ir.Field) []string {
	switch {
	case f.Name == "id" && f.Implicit:
		return nil
	case f.Unique:
		return []string{"CREATE UNIQUE INDEX IF NOT EXISTS " + ident(m.Table+"_"+f.Column+"_key") + " ON " + ident(m.Table) + " (" + ident(f.Column) + ")"}
	case f.Ref != nil || f.Name == "created_at" && f.Implicit:
		return []string{"CREATE INDEX IF NOT EXISTS " + ident(m.Table+"_"+f.Column+"_idx") + " ON " + ident(m.Table) + " (" + ident(f.Column) + ")"}
	}
	return nil
}

// columnType returns the SQLite type that stores t.
func columnType(t *ir.Type) string {
	if t.Kind == ir.Optional {
		t = t.Elem
	}
	switch t.Kind {
	case ir.Int, ir.Bool, ir.Money:
		return "INTEGER"
	case ir.Float:
		return "REAL"
	}
	return "TEXT"
}

// Literal returns a constant as an SQL literal: Money in minor units and
// Bool as 0 or 1.
func Literal(k *ir.Const) string {
	switch v := k.Value.(type) {
	case nil:
		return "NULL"
	case bool:
		if v {
			return "1"
		}
		return "0"
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		s := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEn") {
			s += ".0"
		}
		return s
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	}
	panic(fmt.Sprintf("sqlgen: constant of type %T", k.Value))
}
