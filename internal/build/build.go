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
	"strconv"
	"strings"

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
		return mapErrors(opts.Root, output.String()), nil
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
func mapErrors(root, output string) []diag.Diagnostic {
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
		file := filepath.ToSlash(m[1])
		if strings.HasSuffix(file, ".go") && !strings.HasPrefix(file, Dir+"/") {
			file = Dir + "/" + strings.TrimPrefix(file, "./")
		}
		ln, err1 := strconv.Atoi(m[2])
		col, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil {
			raw = append(raw, line)
			continue
		}
		d := global(diag.V902, m[4])
		d.File = file
		d.Range = diag.Range{Start: diag.Position{Line: ln, Col: col}, End: diag.Position{Line: ln, Col: col + 1}}
		d.Snippet = sourceLine(filepath.Join(root, filepath.FromSlash(file)), ln)
		out = append(out, d)
	}
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
