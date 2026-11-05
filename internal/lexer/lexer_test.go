package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	_ "github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/token"
)

// show renders tokens compactly: keywords and operators as written, names
// and numbers with their text, strings and paths with their parts.
func show(toks []token.Token) string {
	var out []string
	for _, t := range toks {
		out = append(out, showToken(t))
	}
	return strings.Join(out, " ")
}

func showToken(t token.Token) string {
	switch t.Kind {
	case token.Ident, token.TypeIdent, token.Int, token.Float:
		return t.Kind.String() + "(" + t.Text + ")"
	case token.String, token.Path:
		var parts []string
		for _, p := range t.Parts {
			if p.Expr {
				parts = append(parts, "{"+show(p.Tokens[:len(p.Tokens)-1])+"}")
			} else {
				parts = append(parts, strconv.Quote(p.Text))
			}
		}
		return t.Kind.String() + "[" + strings.Join(parts, " ") + "]"
	case token.Newline:
		return "NL"
	}
	return t.Kind.String()
}

func lex(src string) Result {
	return Lex(token.NewFile("main.vibe", []byte(src)))
}

func TestTokens(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"model", "model Book {\n  title: Text min 2 max 200\n  seller: Ref User?\n}\n",
			"model TYPEIDENT(Book) { NL IDENT(title) : TYPEIDENT(Text) min INT(2) max INT(200) NL IDENT(seller) : Ref TYPEIDENT(User) ? NL } NL EOF"},
		{"leading and repeated newlines collapse", "\n\n# header\n\nauth User\n\n\n\nauth X",
			"auth TYPEIDENT(User) NL auth TYPEIDENT(X) EOF"},
		{"newlines inside parens are ignored", "f(a,\n  b\n)\nx",
			"IDENT(f) ( IDENT(a) , IDENT(b) ) NL IDENT(x) EOF"},
		{"newlines inside brackets are ignored", "[\n1\n]",
			"[ INT(1) ] EOF"},
		{"newline after a binary operator continues", "a +\nb ==\nc and\nd contains\ne",
			"IDENT(a) + IDENT(b) == IDENT(c) and IDENT(d) contains IDENT(e) EOF"},
		{"newline after comma and assign continues", "let x =\n  1\nonly list,\nget",
			"let IDENT(x) = INT(1) NL only IDENT(list) , get EOF"},
		{"keyword member does not continue the line", "x.and\ny",
			"IDENT(x) . and NL IDENT(y) EOF"},
		{"a block inside parens has statement newlines", "f(x {\na\nb\n})",
			"IDENT(f) ( IDENT(x) { NL IDENT(a) NL IDENT(b) NL } ) EOF"},
		{"closing brace closes an unclosed paren", "{ f(\n}\nx",
			"{ IDENT(f) ( } NL IDENT(x) EOF"},
		{"comments are not tokens", "a # one\nb #two",
			"IDENT(a) NL IDENT(b) EOF"},
		{"operators", "== != <= >= < > -> ? . , : + - * / % = ( ) [ ]",
			"== != <= >= < > -> ? . , : + - * / % = ( ) [ ] EOF"},
		{"numbers", "0 42 1_000_000 9.99 7.50 1_000.5",
			"INT(0) INT(42) INT(1_000_000) FLOAT(9.99) FLOAT(7.50) FLOAT(1_000.5) EOF"},
		{"int member access", "5.str()",
			"INT(5) . IDENT(str) ( ) EOF"},
		{"names", "created_at _x Book BookReview ID Slug ok",
			"IDENT(created_at) IDENT(_x) TYPEIDENT(Book) TYPEIDENT(BookReview) TYPEIDENT(ID) TYPEIDENT(Slug) IDENT(ok) EOF"},
		{"wrong-case names keep their first-letter class", "myVar MAX_SIZE Book_Review",
			"IDENT(myVar) TYPEIDENT(MAX_SIZE) TYPEIDENT(Book_Review) EOF"},
		{"string interpolation", `"Hello {name}!"`,
			`STRING["Hello " {IDENT(name)} "!"] EOF`},
		{"string escapes", `"a\n\t\"\\\{b}"`,
			`STRING["a\n\t\"\\{b}"] EOF`},
		{"nested strings in interpolation", `"x {f("y {z}")} w"`,
			`STRING["x " {IDENT(f) ( STRING["y " {IDENT(z)}] )} " w"] EOF`},
		{"empty string", `""`,
			`STRING[] EOF`},
		{"route path", "get /books/{id}/publish {",
			`get PATH["books" {IDENT(id)} "publish"] { EOF`},
		{"crud path", "crud Post at /posts/{slug} owner author",
			`crud TYPEIDENT(Post) at PATH["posts" {IDENT(slug)}] owner IDENT(author) EOF`},
		{"test path with expression", "(post /books/{b.id}/publish).status",
			`( post PATH["books" {IDENT(b) . IDENT(id)} "publish"] ) . IDENT(status) EOF`},
		{"path then params", "get /books/search (q: Text?, limit = 20) = x",
			`get PATH["books" "search"] ( IDENT(q) : TYPEIDENT(Text) ? , IDENT(limit) = INT(20) ) = IDENT(x) EOF`},
		{"root path", "get / = 1",
			"get PATH[] = INT(1) EOF"},
		{"path segments with dashes and digits", "get /v2/user-books_x",
			`get PATH["v2" "user-books_x"] EOF`},
		{"division after a member named get", "x.get / 2",
			"IDENT(x) . get / INT(2) EOF"},
		{"division", "a / b",
			"IDENT(a) / IDENT(b) EOF"},
		{"byte order mark", "\uFEFFauth User",
			"auth TYPEIDENT(User) EOF"},
		{"crlf line endings", "auth User\r\nauth X\r\n",
			"auth TYPEIDENT(User) NL auth TYPEIDENT(X) NL EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := lex(tt.src)
			if len(res.Diags) > 0 {
				t.Errorf("unexpected diagnostics: %v", res.Diags)
			}
			if got := show(res.Tokens); got != tt.want {
				t.Errorf("tokens:\n got: %s\nwant: %s", got, tt.want)
			}
		})
	}
}

func TestPositions(t *testing.T) {
	res := lex("model Book {\n  title: Text\n}")
	f := token.NewFile("main.vibe", []byte("model Book {\n  title: Text\n}"))
	want := []string{"1:1-1:6", "1:7-1:11", "1:12-1:13", "1:13-2:1", "2:3-2:8", "2:8-2:9", "2:10-2:14", "2:14-3:1", "3:1-3:2", "3:2-3:2"}
	for i, tok := range res.Tokens {
		s, e := f.Position(tok.Pos), f.Position(tok.End)
		got := fmt.Sprintf("%d:%d-%d:%d", s.Line, s.Col, e.Line, e.Col)
		if i >= len(want) || got != want[i] {
			t.Errorf("token %d (%s) at %s", i, showToken(tok), got)
		}
	}
}

func TestComments(t *testing.T) {
	res := lex("# header  \nauth User # trailing\r\n")
	var got []string
	for _, c := range res.Comments {
		got = append(got, c.Text)
	}
	if want := []string{"# header", "# trailing"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("comments = %q, want %q", got, want)
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		src  string
		want string // code, range, fix texts; one line per diagnostic
		toks string // tokens, or "" to skip the check
	}{
		{`fail "not yours`, `V102 1:6-1:16 ["\""]`, `fail STRING["not yours"] EOF`},
		{"x = \"a {b\"\ny", `V102 1:8-1:11 []`, ""},
		{`"a\qb"`, `V103 1:3-1:5 [""]`, `STRING["aqb"] EOF`},
		{"a && b", `V104 1:3-1:5 ["and"]`, "IDENT(a) and IDENT(b) EOF"},
		{"a || b", `V104 1:3-1:5 ["or"]`, "IDENT(a) or IDENT(b) EOF"},
		{"!a", `V104 1:1-1:2 ["not "]`, "not IDENT(a) EOF"},
		{"let x = 1;", `V104 1:10-1:11 [""]`, "let IDENT(x) = INT(1) EOF"},
		{"a; b", `V104 1:2-1:3 ["\n"]`, "IDENT(a) IDENT(b) EOF"},
		{"'hi {x}'", `V104 1:1-1:9 ["\"hi \\{x}\""]`, `STRING["hi {x}"] EOF`},
		{"// note\nauth User", `V104 1:1-1:3 ["#"]`, "auth TYPEIDENT(User) EOF"},
		{"a @ b", `V104 1:3-1:4 []`, "IDENT(a) IDENT(b) EOF"},
		{"adə", `V104 1:3-1:4 []`, "IDENT(ad) EOF"},
		{"\"\xff\"", `V104 1:2-1:3 []`, ""},
		{`"{x # y}"`, `V104 1:5-1:6 []`, ""},
		{"1__0", `V106 1:1-1:5 []`, "INT(1__0) EOF"},
		{"1_", `V106 1:1-1:3 []`, ""},
		{"0x1F", `V106 1:1-1:5 []`, "INT(0x1F) EOF"},
		{"1e5", `V106 1:1-1:4 []`, ""},
		{"12abc", `V106 1:1-1:6 []`, ""},
		{"99999999999999999999", `V106 1:1-1:21 []`, ""},
		{"x = 1.\n", `V106 1:5-1:7 []`, "IDENT(x) = FLOAT(1.) NL EOF"},
		{"1.5.2", `V106 1:1-1:6 []`, "FLOAT(1.5.2) EOF"},
		{"get /Books", `V109 1:5-1:11 ["books"]`, "get PATH[] EOF"},
		{"get /books/:id", `V109 1:5-1:15 ["{id}"]`, ""},
		{"get /books/ = x", `V109 1:5-1:12 [""]`, `get PATH["books"] = IDENT(x) EOF`},
		{"get /a//b", `V109 1:5-1:10 [""]`, `get PATH["a" "b"] EOF`},
		{"get /books?limit=5", `V109 1:5-1:19 [""]`, ""},
		{"get /a/{id}.json", `V109 1:5-1:17 []`, `get PATH["a"] EOF`},
		{"get /a/{id\nx", `V109 1:5-1:11 []`, ""},
		{"get /a/*", `V109 1:5-1:9 []`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			res := lex(tt.src)
			var got []string
			for _, d := range res.Diags {
				var fixes []string
				for _, f := range d.Fixes {
					for _, e := range f.Edits {
						fixes = append(fixes, strconv.Quote(e.NewText))
					}
				}
				got = append(got, fmt.Sprintf("%s %d:%d-%d:%d [%s]", d.Code,
					d.Range.Start.Line, d.Range.Start.Col, d.Range.End.Line, d.Range.End.Col, strings.Join(fixes, " ")))
			}
			if strings.Join(got, "\n") != tt.want {
				t.Errorf("diagnostics:\n got: %s\nwant: %s\nmessages: %v", strings.Join(got, "\n"), tt.want, res.Diags)
			}
			if tt.toks != "" {
				if got := show(res.Tokens); got != tt.toks {
					t.Errorf("tokens:\n got: %s\nwant: %s", got, tt.toks)
				}
			}
		})
	}
}

func TestQuote(t *testing.T) {
	if got, want := Quote("a\"b\\c{d}\n\tü"), `"a\"b\\c\{d}\n\tü"`; got != want {
		t.Errorf("Quote = %s, want %s", got, want)
	}
	// Lexing a quoted string gives back the original text.
	for _, s := range []string{"", "plain", "{x}", `\{`, "a\nb", `"quoted"`} {
		res := lex(Quote(s))
		var text strings.Builder
		for _, p := range res.Tokens[0].Parts {
			text.WriteString(p.Text)
		}
		if len(res.Diags) > 0 || text.String() != s {
			t.Errorf("lex(Quote(%q)) = %q, %v", s, text.String(), res.Diags)
		}
	}
}
