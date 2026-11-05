package parser

import (
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/token"
)

// TestLargeInputs guards against work that grows faster than the input:
// each case is 200 KB or more and must parse quickly with a bounded number
// of diagnostics. A quadratic regression makes this test take minutes.
func TestLargeInputs(t *testing.T) {
	const n = 200_000
	cases := map[string]string{
		"semicolons on one line":    strings.Repeat(";", n),
		"single quotes on one line": strings.Repeat("'", n),
		"invalid characters":        strings.Repeat("@", n),
		"errors on many lines":      strings.Repeat("x\n", n),
		"unterminated strings":      strings.Repeat("\"abc\n", n/4),
		"wrong-case members":        "fn f() -> Int {\n  return a" + strings.Repeat(".Bb", n/3) + "\n}\n",
		"chained comparisons":       "fn f() -> Bool {\n  return a" + strings.Repeat(" < a", n/4) + "\n}\n",
		"nested parens":             "fn f() -> Int {\n  return " + strings.Repeat("(", n/4) + "1" + strings.Repeat(")", n/4) + "\n}\n",
		"nested interpolation":      "fn f() -> Text {\n  return " + strings.Repeat("\"{", 2_000) + "x" + strings.Repeat("}\"", 2_000) + "\n}\n",
		"open path braces":          "feature F {\n  get /a/" + strings.Repeat("{", n) + "\n}\n",
		"many models":               strings.Repeat("model A {\n  x: Int\n}\n", n/20),
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := ParseFile(token.NewFile("large.vibe", []byte(src)))
			if len(diags) > maxDiags {
				t.Errorf("%d diagnostics, want at most %d", len(diags), maxDiags)
			}
		})
	}
}
