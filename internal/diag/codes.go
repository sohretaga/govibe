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
	V201 Code = "V201"
	V202 Code = "V202"
	V203 Code = "V203"
	V204 Code = "V204"
	V205 Code = "V205"
	V206 Code = "V206"
	V207 Code = "V207"
	V208 Code = "V208"
	V209 Code = "V209"
	V301 Code = "V301"
	V302 Code = "V302"
	V303 Code = "V303"
	V304 Code = "V304"
	V306 Code = "V306"
	V308 Code = "V308"
	V310 Code = "V310"
	V311 Code = "V311"
	V312 Code = "V312"
	V401 Code = "V401"
	V402 Code = "V402"
	V403 Code = "V403"
	V404 Code = "V404"
	V405 Code = "V405"
	V406 Code = "V406"
	V501 Code = "V501"
	V502 Code = "V502"
	V503 Code = "V503"
	V504 Code = "V504"
	V505 Code = "V505"
	V506 Code = "V506"

	V901 Code = "V901"
	V902 Code = "V902"
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
		Code:     V201,
		Title:    "unknown field",
		Template: "%s has no field %s",
		Explanation: `A field name does not exist on the model, in a member access such as
book.titel, in the named arguments of add, update and find, in a where
condition, in order, in an owner clause or in a Slug source. Every model
also has the fields id, created_at and updated_at.`,
		Before: `get /books/search (q: Text) = Book.where(titel contains q)`,
		After:  `get /books/search (q: Text) = Book.where(title contains q)`,
	},
	{
		Code:     V202,
		Title:    "unknown name",
		Template: "unknown name %s%s",
		Explanation: `No local, parameter, function or built-in has this name where it is
used. Locals are visible from their let to the end of their block. A fn
inside a feature is visible only in that feature. me exists only in
routes of an app with auth, and status names such as created exist only
after fail and in comparisons with a response status.`,
		Before: "post /books/{id}/publish {\n  return bok.update(published: true)\n}",
		After:  "post /books/{id}/publish {\n  let book = Book.find(id)\n  return book.update(published: true)\n}",
	},
	{
		Code:     V203,
		Title:    "shadowed name",
		Template: "%s is already declared in this block, at line %s",
		Explanation: `Bindings are immutable, and a name is declared once per block. A route
or function's parameters belong to its outermost block. Pick a new name
for the second value.`,
		Before: "let book = Book.find(id)\nlet book = book.update(published: true)",
		After:  "let book = Book.find(id)\nlet published = book.update(published: true)",
	},
	{
		Code:     V204,
		Title:    "duplicate name",
		Template: "%s is declared twice in %s",
		Explanation: `A name is declared twice where it must be unique: a field in a model, a
parameter in a list, a named argument in a call, a path parameter in a
path, an operation after only, a fn or test in a feature. The fields id,
created_at and updated_at exist on every model and cannot be declared.`,
		Before: "model Book {\n  title: Text\n  title: Text min 2\n}",
		After:  "model Book {\n  title: Text min 2\n}",
	},
	{
		Code:     V205,
		Title:    "ambiguous name in where",
		Template: "%s in the where of %s is both a field and a %s",
		Explanation: `Inside where, a bare name is looked up among the model's fields first,
then among locals. A name that is both is ambiguous: rename the local.`,
		Before: "get /books (title: Text?) = Book.where(title contains title)",
		After:  "get /books (q: Text?) = Book.where(title contains q)",
	},
	{
		Code:     V206,
		Title:    "find by a non-unique field",
		Template: "%s.find cannot use %s, which is not unique",
		Explanation: `M.find(field: value) loads the one record with that value, so the field
must be unique: id, a Slug, or a field marked unique. For other fields use
where, which returns a list, or where(...).first(), which returns an
optional record.`,
		Before: `let book = Book.find(title: t)`,
		After:  `let book = Book.where(title == t).first()`,
	},
	{
		Code:     V207,
		Title:    "duplicate name across files",
		Template: "%s is declared more than once: at %s and at %s",
		Explanation: `Models, features and top-level functions share one project: every .vibe
file sees all of them, so each name is declared once. A feature lives in
one file. The message names both places.`,
		Before: "# books.vibe\nmodel Book {\n  title: Text\n}\n# orders.vibe\nmodel Book {\n  title: Text\n}",
		After:  "# books.vibe\nmodel Book {\n  title: Text\n}",
	},
	{
		Code:     V208,
		Title:    "unknown type",
		Template: "unknown type %s",
		Explanation: `A type name is not a built-in type (Int, Float, Text, Bool, Time, Money,
Email, ID) or a model of the project.`,
		Before: `fn total(n: Integer) -> Integer {`,
		After:  `fn total(n: Int) -> Int {`,
	},
	{
		Code:     V209,
		Title:    "reserved name",
		Template: "%s cannot be named %s: %s",
		Explanation: `Built-in type names cannot name models, and built-in function names (now,
len, str, login_as) cannot name functions.`,
		Before: "model Text {\n  body: Text\n}",
		After:  "model Note {\n  body: Text\n}",
	},
	{
		Code:     V301,
		Title:    "type mismatch",
		Template: "type mismatch: %s",
		Explanation: `A value has a different type than its place needs. Int widens to Float;
Money mixes only with Money and with Int in *; Email and Slug values are
Text; ID is separate. An optional value (T?) must be tested with
x != none before it is used as a T. Text is joined with interpolation,
not +.`,
		Before: `let label = "Total: " + str(total)`,
		After:  `let label = "Total: {total}"`,
	},
	{
		Code:     V302,
		Title:    "wrong arguments",
		Template: "wrong arguments to %s: %s",
		Explanation: `A call has too many or too few arguments, a name its target does not
take, or positional arguments where names are required. add, update and
find by field take named arguments; add needs every required field.`,
		Before: `let book = Book.add(title: "Dune")`,
		After:  `let book = Book.add(title: "Dune", pages: 412, price: 9.99)`,
	},
	{
		Code:     V303,
		Title:    "unknown method",
		Template: "%s has no method %s%s",
		Explanation: `The method does not exist on this kind of value. Models have all, where,
find and add; records have update and delete; queries have order, newest,
limit, offset, first and count; Text has lower, upper, trim, starts_with
and ends_with.`,
		Before: `get /books = Book.where(published).sort(title)`,
		After:  `get /books = Book.where(published).order(title)`,
	},
	{
		Code:     V304,
		Title:    "Money precision",
		Template: "Money value %s has more than 2 decimal places",
		Explanation: `Money is stored in minor units (cents), so a Money literal has at most
two decimal places.`,
		Before: `price: Money min 0.005`,
		After:  `price: Money min 0.01`,
	},
	{
		Code:     V306,
		Title:    "missing return",
		Template: "%s does not return a value on every path",
		Explanation: `A fn must return its result type on every path. A route that returns a
value on some path must return one on every path; fail also ends a path.`,
		Before: "fn sign(n: Int) -> Int {\n  if n < 0 { return -1 }\n}",
		After:  "fn sign(n: Int) -> Int {\n  if n < 0 { return -1 }\n  return 1\n}",
	},
	{
		Code:     V308,
		Title:    "not allowed in a query",
		Template: "not allowed in a query: %s",
		Explanation: `Query conditions compile to SQL at build time. Inside where and order a
field can be compared, tested with contains (field on the left), or used
alone when it is a Bool. Compute other values before the query.`,
		Before: `get /cheap = Book.where(price * 2 < 10)`,
		After:  `get /cheap = Book.where(price < 5)`,
	},
	{
		Code:     V310,
		Title:    "effect in pure fn",
		Template: "fn %s cannot %s: a fn is pure; do this in a route or test",
		Explanation: `In v0.1 a fn only computes a value from its parameters: it cannot run
queries, use me or fail. Do those in the route and pass the values in.`,
		Before: "fn owner_name(id: ID) -> Text {\n  return User.find(id).name\n}",
		After:  "fn greeting(name: Text) -> Text {\n  return \"Hello {name}\"\n}",
	},
	{
		Code:     V311,
		Title:    "extern called from pure fn",
		Template: "fn %s cannot call extern fn %s: an extern may have effects",
		Explanation: `An extern fn is Go code that may call services or the database, so only
routes and tests call it.`,
		Before: "fn pay(amount: Money) -> Text {\n  return charge_card(amount, \"tok\")\n}",
		After:  "post /pay (amount: Money, token: Text) = charge_card(amount, token)",
	},
	{
		Code:     V312,
		Title:    "number after fail",
		Template: "fail takes a status name, not the number %s%s",
		Explanation: `GoVibe code never contains HTTP numbers. fail takes one of invalid (422),
login_required (401), forbidden (403), not_found (404) or conflict (409).`,
		Before: `fail 409 "already published"`,
		After:  `fail conflict "already published"`,
	},
	{
		Code:     V401,
		Title:    "duplicate route",
		Template: "route %s is declared twice: %s and %s",
		Explanation: `Two routes have the same method and path, counting routes that crud and
auth generate and GET /healthz. Parameter names do not matter: /books/{id}
and /books/{slug} are the same path. Run vibe routes to see every route.`,
		Before: "crud Book at /books\nget /books/{id} = Book.find(id)",
		After:  "crud Book at /books",
	},
	{
		Code:     V402,
		Title:    "HTTP call outside test",
		Template: "request %s outside a test: requests are only sent from test blocks",
		Explanation: `get /path, post /path (...) and the other request expressions exist for
tests. A route calls a fn, an extern fn or runs a query instead.`,
		Before: "post /orders {\n  let book = get /books/1\n}",
		After:  "post /orders/{book_id} {\n  let book = Book.find(book_id)\n}",
	},
	{
		Code:     V403,
		Title:    "crud path parameter that names no field",
		Template: "crud %s cannot use path parameter %s: %s",
		Explanation: `The last segment of a crud path may name the lookup key, as in
crud Post at /posts/{slug}. It must name id or a field of the model, and
it must be the last segment: crud cannot fill other path parameters.`,
		Before: `crud Post at /posts/{title_slug}`,
		After:  `crud Post at /posts/{slug}`,
	},
	{
		Code:     V404,
		Title:    "crud key is not unique",
		Template: "crud %s cannot look up records by %s, which is not unique",
		Explanation: `A crud key finds one record, so it must be id, a Slug or a field marked
unique.`,
		Before: "model User {\n  username: Text\n}\ncrud User at /users/{username}",
		After:  "model User {\n  username: Text unique\n}\ncrud User at /users/{username}",
	},
	{
		Code:     V405,
		Title:    "test-only statement outside a test",
		Template: "%s is only allowed in a test",
		Explanation: `expect and login_as exist for test blocks. A route checks a condition with
if and fail instead.`,
		Before: `expect book.pages > 0`,
		After:  `if book.pages <= 0 { fail "a book has pages" }`,
	},
	{
		Code:     V406,
		Title:    "invalid crud owner",
		Template: "crud %s cannot use owner %s: %s",
		Explanation: `owner names a field of the model that is a Ref to the auth model, so
crud can fill it with the logged-in user and check it. It needs an auth
declaration.`,
		Before: "auth User\nmodel Book {\n  seller: Text\n}\ncrud Book at /books owner seller",
		After:  "auth User\nmodel Book {\n  seller: Ref User\n}\ncrud Book at /books owner seller",
	},
	{
		Code:     V501,
		Title:    "unknown Ref target",
		Template: "unknown model %s in Ref",
		Explanation: `Ref M stores the ID of a record of model M, so M must be a model of the
project.`,
		Before: `seller: Ref Usr`,
		After:  `seller: Ref User`,
	},
	{
		Code:     V502,
		Title:    "modifier not valid for type",
		Template: "%s is not valid on %s",
		Explanation: `min and max bound the length of Text and the value of Int, Float and
Money; other types have no bounds. A Slug field has no default, and a
bound or default must have the field's type.`,
		Before: `published: Bool min 0`,
		After:  `published: Bool = false`,
	},
	{
		Code:     V503,
		Title:    "auth model without unique email",
		Template: "auth model %s needs the field 'email: Email unique': %s",
		Explanation: `Logging in finds the account by email, so the auth model has a field
email of type Email marked unique.`,
		Before: "auth User\nmodel User {\n  name: Text\n}",
		After:  "auth User\nmodel User {\n  name: Text\n  email: Email unique\n}",
	},
	{
		Code:     V504,
		Title:    "second auth",
		Template: "only one auth is allowed: %s",
		Explanation: `One model is the account model of an app. Roles belong in a field of it,
such as role: Text.`,
		Before: "auth User\nauth Admin",
		After:  "auth User",
	},
	{
		Code:     V505,
		Title:    "Slug source is not a Text field",
		Template: "Slug(%s) needs a Text field of %s: %s",
		Explanation: `A slug is built from a Text field of the same model, such as a title or a
name.`,
		Before: "model Post {\n  slug: Slug(id)\n}",
		After:  "model Post {\n  title: Text\n  slug: Slug(title)\n}",
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
	{
		Code:     V901,
		Title:    "no Go toolchain found",
		Template: "cannot build: %s",
		Explanation: `vibe build turns the program into Go and compiles it with the go
command, so it needs Go 1.26 or newer on the PATH. Install it from
https://go.dev/dl and run vibe build again; vibe version shows the
toolchain it found.`,
		Before: "$ vibe build\nV901 cannot build: the go command was not found",
		After:  "$ go version\ngo version go1.26.0 linux/amd64\n$ vibe build",
	},
	{
		Code:     V902,
		Title:    "go build failed",
		Template: "go build failed: %s",
		Explanation: `The Go code generated from the program did not compile. A program that
vibe check accepts should always build, so this is a bug in vibe: please
report it with the program and this message. The location points at the
generated file in .vibe/build when it cannot be traced back to a .vibe
line.`,
		Before: "$ vibe build\n.vibe/build/handlers.go:12:3: V902 go build failed: ...",
		After:  "$ vibe check\n$ vibe build",
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
