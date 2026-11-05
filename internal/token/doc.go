// Package token defines the lexical tokens of GoVibe and maps byte
// positions in a source file to 1-based lines and columns.
//
// Columns count Unicode code points, not bytes, so a diagnostic points at
// the same character an editor or an agent sees, even after non-ASCII text
// such as "Dünya" earlier on the line.
package token
