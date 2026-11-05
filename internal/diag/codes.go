package diag

import "fmt"

// Code identifies a kind of diagnostic, such as "V101". The first digit
// names the area: 1 lexing and parsing, 2 names and scope, 3 types,
// 4 endpoints and routes, 5 models and schema, 9 build and toolchain.
type Code string

// Diagnostic codes. Each one is documented in the codes table below.
const (
	V101 Code = "V101"
	V102 Code = "V102"
	V103 Code = "V103"
	V104 Code = "V104"
	V105 Code = "V105"
	V106 Code = "V106"
	V107 Code = "V107"
	V108 Code = "V108"
	V109 Code = "V109"
	V506 Code = "V506"
)

// Info documents one diagnostic code for messages and "vibe explain".
type Info struct {
	Code        Code
	Title       string // short name used in code tables
	Template    string // fmt template that builds the message
	Explanation string
	Before      string // example that triggers the code
	After       string // the same example, fixed
}

// codes is the single definition of every diagnostic code.
var codes = []Info{
	{
		Code:     V101,
		Title:    "unexpected token",
		Template: "expected %s, found %s",
		Explanation: `The parser found a token that the grammar does not allow at this point.
The message names what is allowed and what was found instead.

Common causes:
  - a missing ':' between a field or parameter name and its type
  - a missing '}' or ')' earlier in the file
  - two statements on one line: statements end at a newline
  - a keyword used as a name: keywords cannot name fields, parameters or variables
  - a test, route or crud written outside a feature`,
		Before: "model Book {\n  title Text\n}",
		After:  "model Book {\n  title: Text\n}",
	},
	{
		Code:     V102,
		Title:    "unterminated string",
		Template: "unterminated string: %s",
		Explanation: `A string starts and ends with '"' on the same line. Strings cannot span
lines: write \n for a line break. An interpolation {expr} inside a string
must be closed with '}' before the closing quote; write \{ for a literal
brace.`,
		Before: `fail "not your book`,
		After:  `fail "not your book"`,
	},
	{
		Code:     V103,
		Title:    "invalid escape",
		Template: "invalid escape %s in string",
		Explanation: `Strings support exactly five escapes: \n (newline), \t (tab), \" (quote),
\\ (backslash) and \{ (a literal brace, which would otherwise start an
interpolation). Any other backslash sequence is an error.`,
		Before: `fail "price must be \$0 or more"`,
		After:  `fail "price must be $0 or more"`,
	},
	{
		Code:     V104,
		Title:    "invalid character",
		Template: "invalid character %s: %s",
		Explanation: `The character cannot start any token. Comments start with '#', strings
use double quotes, statements end at a newline instead of ';', and
conditions use the keywords and, or and not instead of &&, || and !.
Outside strings and comments, source text is ASCII.`,
		Before: `if book.published && book.pages > 0 { fail "already published" }`,
		After:  `if book.published and book.pages > 0 { fail "already published" }`,
	},
	{
		Code:     V105,
		Title:    "chained comparison",
		Template: "comparison %s cannot be chained",
		Explanation: `Comparison operators do not chain: a < b < c is not a range check.
Compare twice and join the two comparisons with and.`,
		Before: `if 0 < n < 10 { fail "out of range" }`,
		After:  `if 0 < n and n < 10 { fail "out of range" }`,
	},
	{
		Code:     V106,
		Title:    "invalid number",
		Template: "invalid number %s: %s",
		Explanation: `Numbers are decimal digits with an optional '_' between digits
(1_000_000). A float has digits on both sides of the point (9.99).
Hexadecimal, exponents and integers beyond the 64-bit range are not
supported.`,
		Before: `let limit = 1e3`,
		After:  `let limit = 1_000`,
	},
	{
		Code:     V107,
		Title:    "mixed named and positional arguments",
		Template: "call to %s mixes named and positional arguments",
		Explanation: `In one call, either every argument has a name (title: "Dune") or none
does. Model operations such as add, update and find by field take named
arguments; most built-in functions take positional ones.`,
		Before: `Book.add("Dune", pages: 412)`,
		After:  `Book.add(title: "Dune", pages: 412)`,
	},
	{
		Code:     V108,
		Title:    "wrong naming case",
		Template: "%s must be %s: write %s",
		Explanation: `GoVibe has one naming style. Types, models, features and the app are
PascalCase, like BookReview. Everything else is snake_case, like
created_at: fields, parameters, functions, variables, members and path
parameters. Names use only ASCII letters, digits and '_'.`,
		Before: "model book_review {\n  createdBy: Ref User\n}",
		After:  "model BookReview {\n  created_by: Ref User\n}",
	},
	{
		Code:     V109,
		Title:    "invalid path",
		Template: "invalid path %s: %s",
		Explanation: `A path starts with '/'. Each segment is lowercase letters, digits, '_'
and '-', or a parameter in braces such as {id}. Paths have no trailing
slash, no empty segments and no query string: query values are declared
as route parameters.`,
		Before: `get /Books/:id/ = Book.find(id)`,
		After:  `get /books/{id} = Book.find(id)`,
	},
	{
		Code:     V506,
		Title:    "missing or second app block",
		Template: "the project must have exactly one app block: %s",
		Explanation: `Every project has exactly one app block, in any of its files. It names
the app and may set the database URL. A missing block often means vibe
ran outside the project root: run vibe commands from the folder that
holds the project's .vibe files.`,
		Before: "model Book {\n  title: Text\n}",
		After:  "app Library {\n  db \"sqlite://library.db\"\n}\n\nmodel Book {\n  title: Text\n}",
	},
}

// Lookup returns the documentation of c.
func Lookup(c Code) (Info, bool) {
	for _, info := range codes {
		if info.Code == c {
			return info, true
		}
	}
	return Info{}, false
}

// All returns the documentation of every code in code order.
func All() []Info {
	return append([]Info(nil), codes...)
}

// Format builds the message of c from its template and args.
func (c Code) Format(args ...any) string {
	info, ok := Lookup(c)
	if !ok {
		return fmt.Sprint(args...)
	}
	return fmt.Sprintf(info.Template, args...)
}
