package project

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	_ "github.com/sohretaga/govibe/internal/golden"
)

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"main.vibe", "books/books.vibe", "books/orders/orders.vibe",
		"go/ext.vibe", ".vibe/build/x.vibe", ".git/x.vibe", "books/.hidden/x.vibe",
		".#main.vibe", "notes.txt", "main.vibe.bak", "sub/go/kept.vibe",
	} {
		write(t, root, name, "")
	}
	got, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"books/books.vibe", "books/orders/orders.vibe", "main.vibe", "sub/go/kept.vibe"}
	if !slices.Equal(got, want) {
		t.Errorf("Discover = %q, want %q", got, want)
	}
}

// TestSplitReferenceApp checks that the reference app parses the same
// when every declaration lives in its own file, in nested folders.
func TestSplitReferenceApp(t *testing.T) {
	src, err := os.ReadFile("../../examples/library/main.vibe")
	if err != nil {
		t.Fatal(err)
	}
	whole, diags := Parse([]Source{{Name: "main.vibe", Data: src}})
	if len(diags) > 0 {
		t.Fatalf("reference app: %v", diags)
	}
	root := t.TempDir()
	names := []string{"app.vibe", "auth.vibe", "users/user.vibe", "books/book.vibe", "books/feature/books.vibe"}
	var want []string
	for i, d := range whole[0].Decls {
		f := whole[0].Source
		write(t, root, names[i], f.Text(d.Pos(), d.End())+"\n")
		want = append(want, ast.Dump(d))
	}
	srcs, err := Read(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	files, diags := Parse(srcs)
	if len(diags) > 0 {
		t.Fatalf("split app: %v", diags)
	}
	var got []string
	for _, f := range files {
		for _, d := range f.Decls {
			got = append(got, ast.Dump(d))
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("split app parses differently:\n got %q\nwant %q", got, want)
	}
}

func TestEmptyProject(t *testing.T) {
	_, diags := Parse(nil)
	if len(diags) != 1 || diags[0].Code != diag.V506 {
		t.Errorf("Parse(nil) = %v, want one V506", diags)
	}
}

func TestDiagnosticLimit(t *testing.T) {
	var srcs []Source
	for _, name := range []string{"a.vibe", "b.vibe", "c.vibe"} {
		srcs = append(srcs, Source{Name: name, Data: []byte("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@")})
	}
	_, diags := Parse(srcs)
	if len(diags) != diag.Limit || diags[0].File != "a.vibe" {
		t.Errorf("got %d diagnostics starting in %s, want %d starting in a.vibe", len(diags), diags[0].File, diag.Limit)
	}
}
