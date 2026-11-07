// Package build turns a checked program into a binary: it writes the
// generated Go module to .vibe/build in the project, copies the runtime
// into it, runs go build there and maps go build errors to diagnostics.
//
// The module's go.mod and go.sum are fixed and embedded, so every build
// uses the same, verified versions of the SQLite driver and bcrypt.
package build

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/gogen"
	"github.com/sohretaga/govibe/internal/ir"
	vibe "github.com/sohretaga/govibe/runtime"
)

var (
	//go:embed deps/go.mod.txt
	goMod []byte
	//go:embed deps/go.sum.txt
	goSum []byte
)

// Dir is the build directory, relative to the project root.
const Dir = ".vibe/build"

// Options configures a build.
type Options struct {
	Root   string // the project root
	Output string // the binary to write; relative paths are relative to Root
	Go     string // the go command; "" looks it up on the PATH
}

// Build generates the Go module of prog under Root and compiles it to
// Output. Problems the user can act on, such as a missing toolchain or a
// failed compile, are diagnostics; other failures are errors.
func Build(ctx context.Context, prog *ir.Program, opts Options) ([]diag.Diagnostic, error) {
	goCmd := opts.Go
	if goCmd == "" {
		path, err := exec.LookPath("go")
		if err != nil {
			return []diag.Diagnostic{global(diag.V901, "the go command was not found on the PATH; install Go 1.26 or newer from https://go.dev/dl")}, nil
		}
		goCmd = path
	}
	files, err := gogen.Generate(prog)
	if err != nil {
		return nil, err
	}
	ext, diags, err := externFiles(opts.Root, prog)
	if err != nil {
		return nil, err
	}
	if len(diags) > 0 {
		return diag.Truncate(diags), nil
	}
	for name, data := range ext {
		files[name] = data
	}
	dir := filepath.Join(opts.Root, filepath.FromSlash(Dir))
	if err := Write(dir, files); err != nil {
		return nil, err
	}
	out := opts.Output
	if !filepath.IsAbs(out) {
		out = filepath.Join(opts.Root, out)
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, goCmd, "build", "-trimpath", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-mod=readonly")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, fmt.Errorf("run %s build: %w", goCmd, err)
		}
		return mapErrors(opts.Root, prog, output.String()), nil
	}
	return nil, nil
}

// Write replaces dir with a Go module holding files, the runtime package
// in runtime/, and the fixed go.mod and go.sum.
func Write(dir string, files map[string][]byte) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	all := map[string][]byte{"go.mod": goMod, "go.sum": goSum}
	for name, data := range files {
		all[name] = data
	}
	err := fs.WalkDir(vibe.Source, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") || path == "embed.go" {
			return err
		}
		data, err := fs.ReadFile(vibe.Source, path)
		if err != nil {
			return err
		}
		all["runtime/"+path] = data
		return nil
	})
	if err != nil {
		return err
	}
	for name, data := range all {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// errorLine matches a compiler error: file:line:col: message.
var errorLine = regexp.MustCompile(`^(\S+?\.(?:go|vibe)):(\d+):(\d+): (.*)$`)

// mapErrors turns go build output into V902 diagnostics. An error in a
// .vibe file keeps its location; one in generated code points at the file
// in the build directory.
func mapErrors(root string, prog *ir.Program, output string) []diag.Diagnostic {
	var out []diag.Diagnostic
	var raw []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		m := errorLine.FindStringSubmatch(line)
		if m == nil {
			if line != "" && !strings.HasPrefix(line, "#") {
				raw = append(raw, strings.TrimSpace(line))
			}
			continue
		}
		file := strings.TrimPrefix(filepath.ToSlash(m[1]), "./")
		switch {
		case strings.HasPrefix(file, "ext/"):
			// The Go files of extern fns come from go/.
			file = GoDir + "/" + strings.TrimPrefix(file, "ext/")
		case strings.HasSuffix(file, ".go") && !strings.HasPrefix(file, Dir+"/"):
			file = Dir + "/" + file
		}
		ln, err1 := strconv.Atoi(m[2])
		col, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil {
			raw = append(raw, line)
			continue
		}
		if fn := externAt(prog, file, ln); fn != nil {
			name, sig := gogen.ExternFunc(fn)
			d := global(diag.V903, fn.Name, fmt.Sprintf("the Go function %s in %s/ does not match; want %s (%s)", name, GoDir, sig, m[4]))
			out = append(out, located(root, d, fn.Pos))
			continue
		}
		d := global(diag.V902, m[4])
		d.File = file
		d.Snippet = sourceLine(filepath.Join(root, filepath.FromSlash(file)), ln)
		if n := utf8.RuneCountInString(d.Snippet); d.Snippet != "" && col > n {
			// A line directive names the start of a statement, so a column
			// can run past the end of its line.
			col = n
		}
		d.Range = diag.Range{Start: diag.Position{Line: ln, Col: col}, End: diag.Position{Line: ln, Col: col + 1}}
		out = append(out, d)
	}
	out = dropConsequences(out)
	if len(out) == 0 {
		msg := strings.Join(raw, "; ")
		if msg == "" {
			msg = "go build exited with an error and no message"
		}
		out = append(out, global(diag.V902, msg))
	}
	return diag.Truncate(out)
}

// global returns a diagnostic that belongs to no source line.
func global(code diag.Code, args ...any) diag.Diagnostic {
	return diag.Diagnostic{
		Code:     code,
		Severity: diag.SeverityError,
		Message:  code.Format(args...),
		Fixes:    []diag.Fix{},
		Help:     "vibe explain " + string(code),
	}
}

func sourceLine(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	return strings.TrimRight(lines[n-1], "\r")
}

// dropConsequences removes the errors that only repeat a V903: a call
// to an extern fn whose Go function has the wrong signature fails too.
func dropConsequences(ds []diag.Diagnostic) []diag.Diagnostic {
	var bad []string
	for _, d := range ds {
		if d.Code == diag.V903 {
			_, name, _ := strings.Cut(d.Message, "the Go function ")
			name, _, _ = strings.Cut(name, " ")
			bad = append(bad, "ext."+name)
		}
	}
	return slices.DeleteFunc(ds, func(d diag.Diagnostic) bool {
		return d.Code == diag.V902 && slices.ContainsFunc(bad, func(fn string) bool { return strings.Contains(d.Message, fn) })
	})
}
