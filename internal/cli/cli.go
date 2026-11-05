package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/format"
	"github.com/sohretaga/govibe/internal/project"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// result is the JSON envelope of every command. Error holds a failure
// that is not about the source, such as an unreadable folder.
type result struct {
	OK          bool              `json:"ok"`
	Diagnostics []diag.Diagnostic `json:"diagnostics"`
	Files       []string          `json:"files,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// Check runs "vibe check" on the project in dir: it parses and
// type-checks the project, reports every diagnostic and fails if there is
// an error.
func Check(ctx context.Context, dir string, jsonOut bool, stdout, stderr io.Writer) int {
	srcs, err := project.Read(ctx, dir)
	if err != nil {
		return fail(err, jsonOut, stdout, stderr)
	}
	_, diags := project.Check(srcs)
	r := report(diags)
	if jsonOut {
		return emit(r, stdout, stderr)
	}
	if err := diag.WriteText(stderr, r.Diagnostics); err != nil {
		return ExitFailure
	}
	return code(r)
}

// Fmt runs "vibe fmt" on the project in dir. It rewrites every file that
// is not in canonical form, or with check set, only lists those files and
// fails if there are any. Files that do not parse are left untouched and
// their diagnostics reported.
func Fmt(ctx context.Context, dir string, check, jsonOut bool, stdout, stderr io.Writer) int {
	srcs, err := project.Read(ctx, dir)
	if err != nil {
		return fail(err, jsonOut, stdout, stderr)
	}
	var diags []diag.Diagnostic
	var changed []string
	for _, src := range srcs {
		out, ds := format.Source(src.Name, src.Data)
		diags = append(diags, ds...)
		if diag.HasErrors(ds) || bytes.Equal(out, src.Data) {
			continue
		}
		changed = append(changed, src.Name)
		if !check {
			if err := rewrite(filepath.Join(dir, filepath.FromSlash(src.Name)), out); err != nil {
				return fail(err, jsonOut, stdout, stderr)
			}
		}
	}
	r := report(diag.Truncate(diags))
	r.Files = changed
	if check && len(changed) > 0 {
		r.OK = false
	}
	if jsonOut {
		return emit(r, stdout, stderr)
	}
	for _, name := range changed {
		fmt.Fprintln(stdout, name)
	}
	if check && len(changed) > 0 {
		fmt.Fprintf(stderr, "vibe fmt: %d file(s) not formatted; run vibe fmt\n", len(changed))
	}
	if err := diag.WriteText(stderr, r.Diagnostics); err != nil {
		return ExitFailure
	}
	return code(r)
}

func rewrite(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("rewrite %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil {
		return fmt.Errorf("rewrite %s: %w", path, err)
	}
	return nil
}

func report(diags []diag.Diagnostic) result {
	r := diag.NewReport(diags)
	return result{OK: r.OK, Diagnostics: r.Diagnostics}
}

func code(r result) int {
	if r.OK {
		return ExitOK
	}
	return ExitFailure
}

func emit(r result, stdout, stderr io.Writer) int {
	if r.Diagnostics == nil {
		r.Diagnostics = []diag.Diagnostic{}
	}
	if err := diag.WriteJSON(stdout, r); err != nil {
		fmt.Fprintln(stderr, "vibe:", err)
		return ExitFailure
	}
	return code(r)
}

func fail(err error, jsonOut bool, stdout, stderr io.Writer) int {
	if jsonOut {
		emit(result{Error: err.Error()}, stdout, stderr)
		return ExitFailure
	}
	fmt.Fprintln(stderr, "vibe:", err)
	return ExitFailure
}
