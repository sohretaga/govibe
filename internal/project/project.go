package project

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/parser"
	"github.com/sohretaga/govibe/internal/token"
)

// Source is the content of one .vibe file.
type Source struct {
	Name string // slash-separated path relative to the project root
	Data []byte
}

// Discover returns the slash-separated paths, relative to root, of the
// project's .vibe files in lexical order.
func Discover(ctx context.Context, root string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hidden := strings.HasPrefix(d.Name(), ".") && rel != "."
		if d.IsDir() {
			if hidden || rel == "go" {
				return filepath.SkipDir
			}
			return nil
		}
		if !hidden && d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".vibe") {
			names = append(names, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("find .vibe files in %s: %w", root, err)
	}
	return names, nil
}

// Read discovers and reads the project's .vibe files.
func Read(ctx context.Context, root string) ([]Source, error) {
	names, err := Discover(ctx, root)
	if err != nil {
		return nil, err
	}
	srcs := make([]Source, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		srcs = append(srcs, Source{Name: name, Data: data})
	}
	return srcs, nil
}

// Parse parses every source. A project without files reports V506, since
// it has no app block. At most diag.Limit diagnostics are returned.
func Parse(srcs []Source) ([]*ast.File, []diag.Diagnostic) {
	if len(srcs) == 0 {
		f := token.NewFile("", nil)
		d := diag.New(f, diag.V506, f.EOF(), f.EOF(), "found none, because the project root has no .vibe files")
		return nil, []diag.Diagnostic{d}
	}
	var files []*ast.File
	var diags []diag.Diagnostic
	for _, src := range srcs {
		file, ds := parser.ParseFile(token.NewFile(src.Name, src.Data))
		files = append(files, file)
		diags = append(diags, ds...)
	}
	return files, diag.Truncate(diags)
}
