package lexer

import (
	"testing"

	"github.com/sohretaga/govibe/internal/token"
)

func FuzzLex(f *testing.F) {
	for _, s := range []string{
		"model Book {\n  title: Text min 2\n}\n",
		`get /books/{b.id}/x (q: Text?) = "a {f("b {c}")} d"`,
		"'x' && y || !z; // c",
		"get /a/{\"}\"}/b?x=1 1__2 0x1 1.",
		"\"\\",
		"get /{",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		res := Lex(token.NewFile("fuzz.vibe", []byte(src)))
		checkTokens(t, res.Tokens, len(src))
		for _, d := range res.Diags {
			if d.Range.Start.Line < 1 || d.Range.Start.Col < 1 {
				t.Fatalf("diagnostic without a location: %v", d)
			}
		}
	})
}

func checkTokens(t *testing.T, toks []token.Token, size int) {
	t.Helper()
	if len(toks) == 0 || toks[len(toks)-1].Kind != token.EOF {
		t.Fatalf("tokens do not end with EOF: %v", toks)
	}
	prev := token.PosAt(0)
	for _, tok := range toks {
		if tok.Pos < prev || tok.End < tok.Pos || tok.End.Offset() > size {
			t.Fatalf("token %v out of order or out of range", tok)
		}
		prev = tok.Pos
		for _, p := range tok.Parts {
			if p.Pos < tok.Pos || p.End > tok.End {
				t.Fatalf("part %v outside its token %v", p, tok)
			}
			if p.Expr {
				checkTokens(t, p.Tokens, size)
			}
		}
	}
}
