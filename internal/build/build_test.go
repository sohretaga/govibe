package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/project"
)

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "build")
	if err := os.MkdirAll(filepath.Join(dir, "stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, map[string][]byte{"main.go": []byte("package main\n")}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go.mod", "go.sum", "main.go", "runtime/server.go", "runtime/doc.go"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	for _, name := range []string{"stale", "runtime/embed.go", "runtime/server_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err == nil {
			t.Errorf("%s should not be in the build directory", name)
		}
	}
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil || !strings.Contains(string(mod), "module vibeapp") || !strings.Contains(string(mod), "modernc.org/sqlite v") {
		t.Errorf("go.mod = %s, %v", mod, err)
	}
}

func TestMapErrors(t *testing.T) {
	root := t.TempDir()
	gen := filepath.Join(root, filepath.FromSlash(Dir))
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen, "handlers.go"), []byte("package main\n\nfunc f() { x }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := "# vibeapp\n./handlers.go:3:12: undefined: x\nmain.vibe:7:3: cannot use y\n"
	ds := mapErrors(root, out)
	if len(ds) != 2 {
		t.Fatalf("mapErrors = %+v, want 2 diagnostics", ds)
	}
	g, v := ds[0], ds[1]
	if g.Code != diag.V902 || g.File != ".vibe/build/handlers.go" || g.Range.Start.Line != 3 || g.Range.Start.Col != 12 ||
		g.Message != "go build failed: undefined: x" || g.Snippet != "func f() { x }" {
		t.Errorf("generated-file diagnostic = %+v", g)
	}
	if v.File != "main.vibe" || v.Range.Start.Line != 7 || v.Message != "go build failed: cannot use y" {
		t.Errorf(".vibe diagnostic = %+v", v)
	}

	ds = mapErrors(root, "go: modernc.org/sqlite@v1.60.1: missing go.sum entry\n")
	if len(ds) != 1 || ds[0].File != "" || !strings.Contains(ds[0].Message, "missing go.sum entry") {
		t.Errorf("unlocated error = %+v", ds)
	}
	if ds := mapErrors(root, ""); len(ds) != 1 || !strings.Contains(ds[0].Message, "no message") {
		t.Errorf("empty output = %+v", ds)
	}
}

func TestMissingGo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	prog, diags := project.Check([]project.Source{{Name: "main.vibe", Data: []byte("app A {}\n")}})
	if prog == nil {
		t.Fatal(diags)
	}
	ds, err := Build(context.Background(), prog, Options{Root: t.TempDir(), Output: "a"})
	if err != nil || len(ds) != 1 || ds[0].Code != diag.V901 {
		t.Errorf("Build without go = %+v, %v; want V901", ds, err)
	}
}

func TestUnsupported(t *testing.T) {
	src := "app A {}\nauth User\nmodel User {\n  email: Email unique\n}\n"
	prog, diags := project.Check([]project.Source{{Name: "main.vibe", Data: []byte(src)}})
	if prog == nil {
		t.Fatal(diags)
	}
	_, err := Build(context.Background(), prog, Options{Root: t.TempDir(), Output: "a", Go: "go"})
	if err == nil || !strings.Contains(err.Error(), "does not support auth yet") {
		t.Errorf("Build = %v, want an unsupported error", err)
	}
}
