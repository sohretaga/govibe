package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/token"
)

func TestReferenceApp(t *testing.T) {
	src, err := os.ReadFile("../../examples/library/main.vibe")
	if err != nil {
		t.Fatal(err)
	}
	file, diags := ParseFile(token.NewFile("main.vibe", src))
	for _, d := range diags {
		t.Errorf("%v", d)
	}
	if len(file.Decls) != 5 {
		t.Errorf("got %d declarations, want 5", len(file.Decls))
	}
}

func TestParseGolden(t *testing.T) {
	files, err := filepath.Glob("../../testdata/parse/*.vibe")
	if err != nil || len(files) == 0 {
		t.Fatalf("no parse cases: %v", err)
	}
	files = append(files, "../../examples/library/main.vibe")
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".vibe")
		if strings.HasSuffix(path, "examples/library/main.vibe") {
			name = "library"
		}
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file, diags := ParseFile(token.NewFile(filepath.Base(path), src))
			for _, d := range diags {
				t.Errorf("%v", d)
			}
			ast.InspectFile(file, func(n ast.Node) bool {
				if bad, ok := n.(*ast.Bad); ok {
					t.Errorf("Bad node at %v", file.Source.Position(bad.From))
				}
				return true
			})
			golden.Assert(t, "../../testdata/parse/"+name+".ast.golden", []byte(ast.Dump(file)))
		})
	}
}
