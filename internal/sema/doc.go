// Package sema resolves names and checks types in a parsed project and
// builds its IR. It is the only producer of ir.
//
// Check runs in passes: it collects the top-level names of every file,
// resolves models and auth, resolves function and route signatures,
// expands crud and auth into the route table, and then checks the bodies
// of functions, routes and tests. A type error gives its expression the
// Invalid type, which every later check accepts, so one mistake is
// reported once.
package sema
