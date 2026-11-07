package sema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/parser"
	"github.com/sohretaga/govibe/internal/token"
)

// FuzzCheck checks that every program that parses can be checked without
// a panic and that every diagnostic is located.
func FuzzCheck(f *testing.F) {
	for _, pattern := range []string{"../../testdata/parse/*.vibe", "../../testdata/check/*.vibe", "../../examples/*/*.vibe"} {
		paths, _ := filepath.Glob(pattern)
		for _, path := range paths {
			if src, err := os.ReadFile(path); err == nil {
				f.Add(string(src))
			}
		}
	}
	f.Add(shop)
	f.Fuzz(func(t *testing.T, src string) {
		file, diags := parser.ParseFile(token.NewFile("fuzz.vibe", []byte(src)))
		if len(diags) > 0 {
			return
		}
		_, diags = Check([]*ast.File{file})
		for _, d := range diags {
			if d.Range.Start.Line < 1 || d.Message == "" {
				t.Fatalf("diagnostic without location or message: %+v", d)
			}
		}
	})
}
