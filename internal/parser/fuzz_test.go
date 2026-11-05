package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/token"
)

func FuzzParseFile(f *testing.F) {
	paths, _ := filepath.Glob("../../testdata/parse/*.vibe")
	for _, path := range paths {
		if src, err := os.ReadFile(path); err == nil {
			f.Add(string(src))
		}
	}
	for _, s := range []string{
		"model A {\n  x: Int }",
		"feature F {\n  post /a {\n    if x {\n    }\n    else {\n    }\n  }\n}",
		"fn f() -> Int {\n  return g(1, b: 2) < 3 < 4\n}",
		"feature F {\n  get /a/{b.c} = \"{\"{}\"}\"\n}",
		"}}}{{{",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		file, diags := ParseFile(token.NewFile("fuzz.vibe", []byte(src)))
		bad := false
		ast.InspectFile(file, func(n ast.Node) bool {
			if _, ok := n.(*ast.Bad); ok {
				bad = true
			}
			if n.End() < n.Pos() {
				t.Fatalf("%T ends before it starts", n)
			}
			return true
		})
		if bad && len(diags) == 0 {
			t.Fatal("Bad node without a diagnostic")
		}
		for _, d := range diags {
			if d.Range.Start.Line < 1 || d.Message == "" {
				t.Fatalf("diagnostic without location or message: %+v", d)
			}
		}
	})
}
