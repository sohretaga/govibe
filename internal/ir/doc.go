// Package ir is the typed, resolved model of a GoVibe program. The sema
// package is its only producer, and code generation reads only ir, never
// the syntax tree.
//
// Every expression carries its type. Routes form one table in which crud
// and auth declarations are expanded into ordinary routes. Queries are
// structured plans that sqlgen turns into static SQL.
package ir
