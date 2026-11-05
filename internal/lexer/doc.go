// Package lexer converts GoVibe source text into tokens.
//
// The lexer applies the newline rules of the spec: it emits one Newline
// token per line end, collapses consecutive newlines, and drops newlines
// inside ( ) and [ ] and after a binary operator, ',' or '='. Paths are
// lexed only right after an HTTP method keyword or "at". The {expr}
// sections of strings and paths are lexed into the Parts of their token.
//
// Lexing never stops at an error: the lexer reports a diagnostic and
// emits the most plausible token, so the parser can keep going.
package lexer
