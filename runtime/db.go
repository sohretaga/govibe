package vibe

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// driverName is the database/sql driver the generated main package
// imports: modernc.org/sqlite.
const driverName = "sqlite"

// DB is the app's SQLite database: one writer connection and a pool of
// readers, with every query of the app prepared once at startup.
type DB struct {
	write, read *sql.DB
	stmts       map[string]*sql.Stmt
	fields      map[string]string // "table.column" to the field name
}

// dbPath returns the file a database URL names, or ":memory:".
func dbPath(url string) (string, error) {
	path, ok := strings.CutPrefix(url, "sqlite://")
	if !ok || path == "" {
		return "", fmt.Errorf("database URL %q: want sqlite://<file>, such as sqlite://app.db", url)
	}
	if strings.ContainsAny(path, "?#") {
		return "", fmt.Errorf("database URL %q: the file name cannot contain ? or #", url)
	}
	return path, nil
}

const pragmas = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"

// openDB opens the database at url. A file database runs in WAL mode with
// one writer and several readers; an in-memory one has one connection,
// since every connection to :memory: is a separate database.
func openDB(ctx context.Context, url string, tables []Table) (*DB, error) {
	path, err := dbPath(url)
	if err != nil {
		return nil, err
	}
	db := &DB{stmts: map[string]*sql.Stmt{}, fields: map[string]string{}}
	for _, t := range tables {
		for _, c := range t.Columns {
			db.fields[t.Name+"."+c.Name] = c.Field
		}
	}
	if path == ":memory:" {
		db.write, err = sql.Open(driverName, ":memory:?"+pragmas)
		if err != nil {
			return nil, err
		}
		db.write.SetMaxOpenConns(1)
		db.write.SetMaxIdleConns(1)
		db.write.SetConnMaxLifetime(0)
		db.read = db.write
	} else {
		db.write, err = sql.Open(driverName, path+"?"+pragmas+"&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_txlock=immediate")
		if err != nil {
			return nil, err
		}
		db.write.SetMaxOpenConns(1)
		db.read, err = sql.Open(driverName, path+"?"+pragmas+"&_pragma=query_only(1)")
		if err != nil {
			return nil, errors.Join(err, db.write.Close())
		}
		db.read.SetMaxOpenConns(max(4, runtime.NumCPU()))
	}
	if err := db.write.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("open %s: %w", path, err), db.Close())
	}
	return db, nil
}

// Close closes both pools.
func (db *DB) Close() error {
	err := db.write.Close()
	if db.read != db.write {
		err = errors.Join(err, db.read.Close())
	}
	return err
}

// writes reports whether a statement changes the database, so it runs on
// the writer.
func writes(query string) bool {
	return !strings.HasPrefix(query, "SELECT ")
}

func (db *DB) pool(query string) *sql.DB {
	if writes(query) {
		return db.write
	}
	return db.read
}

// prepare prepares every query once, which also checks them against the
// schema at startup.
func (db *DB) prepare(ctx context.Context, queries []string) error {
	for _, q := range queries {
		if _, ok := db.stmts[q]; ok {
			continue
		}
		st, err := db.pool(q).PrepareContext(ctx, q)
		if err != nil {
			return fmt.Errorf("prepare %q: %w", q, err)
		}
		db.stmts[q] = st
	}
	return nil
}

func (db *DB) stmt(query string) (*sql.Stmt, error) {
	if st, ok := db.stmts[query]; ok {
		return st, nil
	}
	return nil, fmt.Errorf("vibe: query was not prepared at startup: %q", query)
}

// Rows runs a query that returns rows.
func (db *DB) Rows(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	st, err := db.stmt(query)
	if err != nil {
		return nil, err
	}
	rows, err := st.QueryContext(ctx, args...)
	if err != nil {
		return nil, db.mapErr(err)
	}
	return rows, nil
}

// Row runs a query that returns at most one row. Its Scan answers
// sql.ErrNoRows when there is none.
func (db *DB) Row(ctx context.Context, query string, args ...any) *sql.Row {
	st, err := db.stmt(query)
	if err != nil {
		// A Row cannot hold an error of its own; run the query unprepared
		// so the error surfaces from Scan.
		return db.pool(query).QueryRowContext(ctx, query, args...)
	}
	return st.QueryRowContext(ctx, args...)
}

// Exec runs a statement that changes the database. A broken unique rule
// answers conflict and names the field.
func (db *DB) Exec(ctx context.Context, query string, args ...any) error {
	st, err := db.stmt(query)
	if err != nil {
		return err
	}
	if _, err := st.ExecContext(ctx, args...); err != nil {
		return db.mapErr(err)
	}
	return nil
}

// Exists reports whether a query finds a row.
func (db *DB) Exists(ctx context.Context, query string, args ...any) (bool, error) {
	var one int64
	err := db.Row(ctx, query, args...).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, db.mapErr(err)
	}
	return true, nil
}

// mapErr turns constraint errors into the statuses the spec names.
func (db *DB) mapErr(err error) error {
	msg := err.Error()
	if _, cols, ok := strings.Cut(msg, "UNIQUE constraint failed: "); ok {
		// The driver appends the SQLite result code, as in
		// "book.slug (2067)".
		if i := strings.LastIndex(cols, " ("); i >= 0 {
			cols = cols[:i]
		}
		fields := map[string]string{}
		for _, c := range strings.Split(cols, ", ") {
			if f, ok := db.fields[strings.TrimSpace(c)]; ok {
				fields[f] = "is already taken"
			}
		}
		return &Error{Status: Conflict, Message: "a record with this value already exists", Fields: fields, Cause: err}
	}
	if strings.Contains(msg, "FOREIGN KEY constraint failed") {
		return &Error{Status: Conflict, Message: "other records still refer to this record", Cause: err}
	}
	return err
}
