// Package format prints a parsed .vibe file in the one canonical layout
// that "vibe fmt" enforces.
//
// The layout follows the spec's examples:
//
//   - two spaces of indentation per block;
//   - exactly one blank line between top-level declarations and between
//     the items of a feature;
//   - model fields and statements keep blank lines from the source,
//     collapsed to one, but never directly after '{' or before '}';
//   - an if without else whose block holds a single fail or return sits
//     on one line, as in if x { fail "no" }; every other block is split
//     over lines, and an empty block prints as {};
//   - an expression always prints on one line, so line breaks and trailing
//     commas inside ( ) and [ ] are dropped, and a comment inside an
//     expression moves to its own line above it;
//   - numbers keep their spelling, such as 7.50 or 1_000, while strings are
//     printed with canonical escapes.
//
// Comments are never dropped: each one prints on its own line or, when it
// followed code on the same line, after that code.
package format
