// Package gogen turns the IR into the Go source of an app: main.go,
// models.go, queries.go, handlers.go and fns.go. It reads only ir and
// the SQL that sqlgen builds from it.
//
// Every file is run through go/format, so generated code is gofmt-clean.
// Expressions that can fail, such as queries, are lifted into statements
// before the expression that uses them, with an early return of the
// error; and and or keep their short-circuit order when the right side
// needs such statements.
package gogen
