package token

import "strconv"

// Kind is the lexical class of a token.
type Kind int

// Token kinds. Operator and keyword kinds print as their source spelling.
const (
	Illegal Kind = iota
	EOF
	Newline
	Comment

	Ident     // snake_case name
	TypeIdent // PascalCase name
	Int       // 42, 1_000
	Float     // 9.99
	String    // "Hello {name}"
	Path      // /books/{id}

	Plus      // +
	Minus     // -
	Star      // *
	Slash     // /
	Percent   // %
	Eq        // ==
	NotEq     // !=
	Less      // <
	LessEq    // <=
	Greater   // >
	GreaterEq // >=
	Assign    // =
	Arrow     // ->
	Question  // ?
	Dot       // .
	LParen    // (
	RParen    // )
	LBrack    // [
	RBrack    // ]
	LBrace    // {
	RBrace    // }
	Comma     // ,
	Colon     // :

	keywordBeg
	App
	DB
	Model
	Feature
	Crud
	At
	Only
	Owner
	Private
	Auth
	Fn
	Extern
	Test
	Get
	Post
	Patch
	Delete
	Let
	If
	Else
	Return
	Fail
	Expect
	And
	Or
	Not
	Contains
	True
	False
	None
	Me
	Ref
	Unique
	Min
	Max
	keywordEnd
)

var kindNames = [...]string{
	Illegal:   "ILLEGAL",
	EOF:       "EOF",
	Newline:   "NEWLINE",
	Comment:   "COMMENT",
	Ident:     "IDENT",
	TypeIdent: "TYPEIDENT",
	Int:       "INT",
	Float:     "FLOAT",
	String:    "STRING",
	Path:      "PATH",
	Plus:      "+",
	Minus:     "-",
	Star:      "*",
	Slash:     "/",
	Percent:   "%",
	Eq:        "==",
	NotEq:     "!=",
	Less:      "<",
	LessEq:    "<=",
	Greater:   ">",
	GreaterEq: ">=",
	Assign:    "=",
	Arrow:     "->",
	Question:  "?",
	Dot:       ".",
	LParen:    "(",
	RParen:    ")",
	LBrack:    "[",
	RBrack:    "]",
	LBrace:    "{",
	RBrace:    "}",
	Comma:     ",",
	Colon:     ":",
	App:       "app",
	DB:        "db",
	Model:     "model",
	Feature:   "feature",
	Crud:      "crud",
	At:        "at",
	Only:      "only",
	Owner:     "owner",
	Private:   "private",
	Auth:      "auth",
	Fn:        "fn",
	Extern:    "extern",
	Test:      "test",
	Get:       "get",
	Post:      "post",
	Patch:     "patch",
	Delete:    "delete",
	Let:       "let",
	If:        "if",
	Else:      "else",
	Return:    "return",
	Fail:      "fail",
	Expect:    "expect",
	And:       "and",
	Or:        "or",
	Not:       "not",
	Contains:  "contains",
	True:      "true",
	False:     "false",
	None:      "none",
	Me:        "me",
	Ref:       "Ref",
	Unique:    "unique",
	Min:       "min",
	Max:       "max",
}

// String returns the source spelling of operators and keywords and an
// upper-case name for every other kind.
func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// IsKeyword reports whether k is one of the 35 keywords.
func (k Kind) IsKeyword() bool { return k > keywordBeg && k < keywordEnd }

// IsMethod reports whether k is an HTTP method keyword.
func (k Kind) IsMethod() bool { return k == Get || k == Post || k == Patch || k == Delete }

// IsComparison reports whether k is a comparison operator, including
// the keyword contains.
func (k Kind) IsComparison() bool {
	switch k {
	case Eq, NotEq, Less, LessEq, Greater, GreaterEq, Contains:
		return true
	}
	return false
}

// IsBinary reports whether k is a binary operator. A newline after a
// binary operator continues the expression on the next line.
func (k Kind) IsBinary() bool {
	switch k {
	case Plus, Minus, Star, Slash, Percent, And, Or:
		return true
	}
	return k.IsComparison()
}

// Lookup returns the keyword kind for word, or Illegal if word is not a
// keyword.
func Lookup(word string) Kind {
	for k := keywordBeg + 1; k < keywordEnd; k++ {
		if kindNames[k] == word {
			return k
		}
	}
	return Illegal
}

// Keywords returns the spelling of every keyword in declaration order.
func Keywords() []string {
	words := make([]string, 0, keywordEnd-keywordBeg-1)
	for k := keywordBeg + 1; k < keywordEnd; k++ {
		words = append(words, kindNames[k])
	}
	return words
}

// Token is one lexical token.
type Token struct {
	Kind Kind
	Pos  Pos    // first byte of the token
	End  Pos    // first byte after the token
	Text string // source text of the token

	// Parts holds the pieces of a String or Path token: literal text and
	// embedded {expr} sections, in source order.
	Parts []Part
}

// Part is a piece of a String or Path token.
type Part struct {
	Pos  Pos // first byte of the piece; for Expr parts, the byte after '{'
	End  Pos // first byte after the piece; for Expr parts, the '}'
	Expr bool

	// Text is the decoded text of a literal piece.
	Text string

	// Tokens holds the tokens of an Expr piece, ending with EOF.
	Tokens []Token
}
