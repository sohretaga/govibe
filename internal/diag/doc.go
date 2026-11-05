// Package diag defines diagnostics: located, coded messages with optional
// fixes that the compiler reports for user mistakes.
//
// Every code is declared once in codes.go with its message template,
// explanation and example. Diagnostics are printed for humans by WriteText
// and for agents as the JSON envelope of "vibe check --json" by WriteJSON.
package diag
