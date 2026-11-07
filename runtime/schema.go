package vibe

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Table describes the table of a model. Its SQL is built by the compiler.
type Table struct {
	Name    string
	Model   string
	Create  string   // CREATE TABLE statement
	Columns []Column // in table order
	Indexes []string // CREATE INDEX IF NOT EXISTS statements
}

// Column describes one column of a table.
type Column struct {
	Name    string
	Field   string // the GoVibe field the column stores
	Type    string // INTEGER, REAL or TEXT
	NotNull bool
	Add     string // ALTER TABLE statement that adds the column to a table with rows, or ""
}

// sqlDB is what schema sync needs from a database or transaction.
type sqlDB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type existingColumn struct {
	typ     string
	notNull bool
	hasDflt bool
}

// syncSchema creates missing tables, columns and indexes. It never drops
// or changes a column that holds data: a difference it cannot apply
// stops startup with a message naming the model and field. An empty table
// whose columns differ is created again.
func syncSchema(ctx context.Context, db *sql.DB, tables []Table) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
			return
		}
		err = tx.Commit()
	}()
	if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON"); err != nil {
		return err
	}
	for _, t := range tables {
		if err := syncTable(ctx, tx, t); err != nil {
			return err
		}
	}
	return nil
}

func syncTable(ctx context.Context, db sqlDB, t Table) error {
	have, err := columns(ctx, db, t.Name)
	if err != nil {
		return err
	}
	if have == nil {
		return create(ctx, db, t)
	}
	empty, err := isEmpty(ctx, db, t.Name)
	if err != nil {
		return err
	}
	var problems, adds []string
	want := map[string]bool{}
	for _, c := range t.Columns {
		want[c.Name] = true
		h, ok := have[c.Name]
		switch {
		case !ok && c.Add == "":
			problems = append(problems, fmt.Sprintf("%s.%s is new and required, but the table has rows; give it a default or make it optional", t.Model, c.Field))
		case !ok:
			adds = append(adds, c.Add)
		case !strings.EqualFold(h.typ, c.Type):
			problems = append(problems, fmt.Sprintf("%s.%s is stored as %s, but the model needs %s", t.Model, c.Field, h.typ, c.Type))
		case h.notNull != c.NotNull:
			problems = append(problems, fmt.Sprintf("%s.%s changed between optional and required", t.Model, c.Field))
		}
	}
	for name, h := range have {
		if !want[name] && h.notNull && !h.hasDflt {
			problems = append(problems, fmt.Sprintf("column %s.%s is required in the database but no longer in model %s", t.Name, name, t.Model))
		}
	}
	if len(problems) > 0 {
		if !empty {
			return fmt.Errorf("the database does not match model %s, and v0.1 never changes columns that hold data:\n  %s",
				t.Model, strings.Join(problems, "\n  "))
		}
		if _, err := db.ExecContext(ctx, "DROP TABLE "+quote(t.Name)); err != nil {
			return err
		}
		return create(ctx, db, t)
	}
	for _, a := range adds {
		if _, err := db.ExecContext(ctx, a); err != nil {
			return fmt.Errorf("model %s: %w", t.Model, err)
		}
	}
	return indexes(ctx, db, t)
}

func create(ctx context.Context, db sqlDB, t Table) error {
	if _, err := db.ExecContext(ctx, t.Create); err != nil {
		return fmt.Errorf("create table %s: %w", t.Name, err)
	}
	return indexes(ctx, db, t)
}

func indexes(ctx context.Context, db sqlDB, t Table) error {
	for _, ix := range t.Indexes {
		if _, err := db.ExecContext(ctx, ix); err != nil {
			return fmt.Errorf("model %s: %w", t.Model, err)
		}
	}
	return nil
}

// columns returns the columns of a table, or nil when it does not exist.
func columns(ctx context.Context, db sqlDB, table string) (map[string]existingColumn, error) {
	rows, err := db.QueryContext(ctx, "SELECT name, type, \"notnull\", dflt_value IS NOT NULL FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out map[string]existingColumn
	for rows.Next() {
		var name string
		var c existingColumn
		if err := rows.Scan(&name, &c.typ, &c.notNull, &c.hasDflt); err != nil {
			return nil, err
		}
		if out == nil {
			out = map[string]existingColumn{}
		}
		out[name] = c
	}
	return out, rows.Err()
}

func isEmpty(ctx context.Context, db sqlDB, table string) (bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT 1 FROM "+quote(table)+" LIMIT 1")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	has := rows.Next()
	return !has, rows.Err()
}

// quote quotes a table name for SQL.
func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
