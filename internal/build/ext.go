package build

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/gogen"
	"github.com/sohretaga/govibe/internal/ir"
)

// GoDir is the folder of the Go files of extern fns, relative to the
// project root.
const GoDir = "go"

// externFiles reads the Go files of extern fns for the build's ext
// package. It reports V903 for each extern fn whose Go function is
// missing and for a file that is not in package ext; a function whose
// signature differs is reported when it compiles.
func externFiles(root string, prog *ir.Program) (map[string][]byte, []diag.Diagnostic, error) {
	externs := gogen.Externs(prog)
	if len(externs) == 0 {
		return nil, nil, nil
	}
	dir := filepath.Join(root, GoDir)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	files := map[string][]byte{}
	funcs := map[string]bool{}
	var diags []diag.Diagnostic
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, nil, err
		}
		files["ext/"+name] = data
		f, err := parser.ParseFile(fset, GoDir+"/"+name, data, parser.SkipObjectResolution)
		if err != nil {
			// go build reports the syntax error with its location.
			continue
		}
		if f.Name.Name != "ext" {
			d := global(diag.V903, externs[0].Name, fmt.Sprintf("%s/%s is package %s; the Go files of extern fns are package ext", GoDir, name, f.Name.Name))
			at := fset.Position(f.Name.Pos())
			d.File, d.Range = GoDir+"/"+name, lineRange(at.Line, at.Column, len(f.Name.Name))
			d.Snippet = sourceLine(filepath.Join(dir, name), at.Line)
			diags = append(diags, d)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				funcs[fn.Name.Name] = true
			}
		}
	}
	for _, fn := range externs {
		name, sig := gogen.ExternFunc(fn)
		if funcs[name] {
			continue
		}
		d := global(diag.V903, fn.Name, fmt.Sprintf("no Go function %s in %s/; add %s to a .go file there, in package ext", name, GoDir, sig))
		diags = append(diags, located(root, d, fn.Pos))
	}
	return files, diags, nil
}

// externAt returns the extern fn declared at file:line, or nil.
func externAt(prog *ir.Program, file string, line int) *ir.Func {
	i := slices.IndexFunc(gogen.Externs(prog), func(fn *ir.Func) bool { return fn.Pos.File == file && fn.Pos.Line == line })
	if i < 0 {
		return nil
	}
	return gogen.Externs(prog)[i]
}

// located puts d at p in a .vibe file of the project.
func located(root string, d diag.Diagnostic, p ir.Pos) diag.Diagnostic {
	d.File = p.File
	d.Snippet = sourceLine(filepath.Join(root, filepath.FromSlash(p.File)), p.Line)
	d.Range = lineRange(p.Line, p.Col, len(strings.TrimSpace(d.Snippet)))
	return d
}

// lineRange returns the range of n characters at line:col.
func lineRange(line, col, n int) diag.Range {
	return diag.Range{Start: diag.Position{Line: line, Col: col}, End: diag.Position{Line: line, Col: col + max(n, 1)}}
}
