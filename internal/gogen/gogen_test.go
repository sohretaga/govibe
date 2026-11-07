package gogen

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/project"
)

func load(t *testing.T, name string, src []byte) *ir.Program {
	t.Helper()
	prog, diags := project.Check([]project.Source{{Name: name, Data: src}})
	for _, d := range diags {
		t.Errorf("%v", d)
	}
	if prog == nil {
		t.FailNow()
	}
	return prog
}

// TestGolden generates every program in testdata/gen and compares the
// files, joined in name order, with the golden file.
func TestGolden(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/gen/*.vibe")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no programs in testdata/gen: %v", err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".vibe")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			files, err := Generate(load(t, "main.vibe", src))
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for n := range files {
				names = append(names, n)
			}
			slices.Sort(names)
			var b strings.Builder
			for _, n := range names {
				b.WriteString("-- " + n + " --\n")
				b.Write(files[n])
			}
			golden.Assert(t, strings.TrimSuffix(path, ".vibe")+".go.golden", []byte(b.String()))
		})
	}
}

// TestDirectivesSurviveGofmt checks that the line directives keep every
// generated file gofmt-clean.
func TestDirectivesSurviveGofmt(t *testing.T) {
	paths, _ := filepath.Glob("../../testdata/gen/*.vibe")
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files, err := Generate(load(t, "main.vibe", src))
		if err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			out, err := format.Source(data)
			if err != nil || !bytes.Equal(out, data) {
				t.Errorf("%s %s is not gofmt-clean: %v", path, name, err)
			}
		}
	}
}

func TestLineDirectives(t *testing.T) {
	src := "package main\n\nfunc f() {\n\tx := 1\n\t//vibe:at main.vibe:7:3\n\tif x > 0 {\n\t\t//vibe:at main.vibe:8:5\n\t\tx++\n\t\t//vibe:end\n\t}\n\t//vibe:end\n\ty := x\n\t_ = y\n}\n"
	want := "package main\n\nfunc f() {\n\tx := 1\n\t/*line main.vibe:7:2*/ if x > 0 {\n\t\t/*line main.vibe:8:4*/ x++\n\t}\n\t/*line handlers.go:8:1*/ y := x\n\t_ = y\n}\n"
	if got := string(lineDirectives("handlers.go", []byte(src))); got != want {
		t.Errorf("lineDirectives =\n%s\nwant\n%s", got, want)
	}
}

func TestPascal(t *testing.T) {
	for in, want := range map[string]string{"title": "Title", "created_at": "CreatedAt", "id": "ID", "seller_id": "SellerID", "a_b1": "AB1"} {
		if got := pascal(in); got != want {
			t.Errorf("pascal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParen(t *testing.T) {
	for in, want := range map[string]string{
		"x":            "x",
		"f(a, b)":      "f(a, b)",
		"a + b":        "(a + b)",
		`"a b"`:        `"a b"`,
		`"a \" b"`:     `"a \" b"`,
		`f("a") + "b"`: `(f("a") + "b")`,
		"*p":           "(*p)",
		"!ok":          "(!ok)",
	} {
		if got := paren(in); got != want {
			t.Errorf("paren(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReserved(t *testing.T) {
	for _, n := range []string{"c", "err", "vibe", "len", "type", "string", "t1", "t20"} {
		if !isReserved(n) {
			t.Errorf("isReserved(%q) = false", n)
		}
	}
	for _, n := range []string{"book", "title", "t", "total", "t1x"} {
		if isReserved(n) {
			t.Errorf("isReserved(%q) = true", n)
		}
	}
}
