// Package sqlgen turns the IR into SQLite SQL at compile time: the schema
// of every model, and one static, parameterized statement per query site.
// The runtime only binds parameters and scans rows; it never builds SQL.
//
// Parameters are numbered (?1, ?2, ...), so a value used twice in a
// statement, such as an optional filter that is tested against NULL and
// then compared, is bound once.
package sqlgen
