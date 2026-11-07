package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"runtime"

	"github.com/sohretaga/govibe/internal/build"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/project"
)

// Build runs "vibe build" on the project in dir: it checks the project,
// generates Go in .vibe/build and compiles it to output, which defaults
// to the app name in snake_case. It prints the path of the binary.
func Build(ctx context.Context, dir, output string, jsonOut bool, stdout, stderr io.Writer) int {
	srcs, err := project.Read(ctx, dir)
	if err != nil {
		return fail(err, jsonOut, stdout, stderr)
	}
	prog, diags := project.Check(srcs)
	if prog != nil {
		if output == "" {
			output = "." + string(filepath.Separator) + prog.App.Snake
			if runtime.GOOS == "windows" {
				output += ".exe"
			}
		}
		more, err := build.Build(ctx, prog, build.Options{Root: dir, Output: output})
		if err != nil {
			return fail(err, jsonOut, stdout, stderr)
		}
		diags = append(diags, more...)
	}
	r := report(diags)
	if r.OK {
		r.Output = output
	}
	if jsonOut {
		return emit(r, stdout, stderr)
	}
	if err := diag.WriteText(stderr, r.Diagnostics); err != nil {
		return ExitFailure
	}
	if r.OK {
		fmt.Fprintln(stdout, output)
	}
	return code(r)
}
