// Package parser builds the syntax tree of a .vibe file.
//
// The parser is hand-written recursive descent with binding powers for
// expressions; the grammar in the spec is normative. It never stops at the
// first error: after a syntax error it skips to the next line or to the '}'
// that closes the current block, and reports at most one syntax error per
// line so that one mistake does not cascade. Where the intended code is
// clear, a diagnostic carries a fix.
package parser
