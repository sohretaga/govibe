// Package ast declares the syntax tree of GoVibe source files.
//
// Every node records the positions of its tokens so that diagnostics and
// the formatter can point back at the source. Parts that failed to parse
// are *Bad nodes; a file without diagnostics contains none.
package ast
