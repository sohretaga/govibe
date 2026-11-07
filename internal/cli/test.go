package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/project"
	"github.com/sohretaga/govibe/internal/testrun"
)

// Test runs "vibe test" on the project in dir: it checks the project,
// builds the app with its tests and runs the tests whose name matches
// the regular expression run. It fails when a test fails or the project
// does not build.
func Test(ctx context.Context, dir, run string, jsonOut bool, stdout, stderr io.Writer) int {
	srcs, err := project.Read(ctx, dir)
	if err != nil {
		return fail(err, jsonOut, stdout, stderr)
	}
	prog, diags := project.Check(srcs)
	var results []testrun.Result
	if prog != nil {
		var more []diag.Diagnostic
		results, more, err = testrun.Run(ctx, prog, testrun.Options{Root: dir, Run: run})
		if err != nil {
			return fail(err, jsonOut, stdout, stderr)
		}
		diags = append(diags, more...)
	}
	r := report(diags)
	if results == nil {
		results = []testrun.Result{}
	}
	r.Tests = &results
	for _, t := range results {
		r.OK = r.OK && t.Passed
	}
	if jsonOut {
		return emit(r, stdout, stderr)
	}
	if err := diag.WriteText(stderr, r.Diagnostics); err != nil {
		return ExitFailure
	}
	if diag.HasErrors(r.Diagnostics) {
		return ExitFailure
	}
	writeTests(stdout, results)
	return code(r)
}

// writeTests prints one line per test, the failure of each failed test
// and a summary.
func writeTests(w io.Writer, results []testrun.Result) {
	failed := 0
	for _, t := range results {
		if t.Passed {
			fmt.Fprintf(w, "PASS  %s (%d ms)\n", t.Name, t.DurationMS)
			continue
		}
		failed++
		fmt.Fprintf(w, "FAIL  %s (%d ms)\n", t.Name, t.DurationMS)
		if f := t.Failure; f != nil {
			msg := strings.ReplaceAll(f.Message, "\n", "\n      ")
			if f.File != "" {
				msg = fmt.Sprintf("%s:%d: %s", f.File, f.Line, msg)
			}
			fmt.Fprintf(w, "      %s\n", msg)
			if f.Left != "" || f.Right != "" {
				fmt.Fprintf(w, "        left:  %s\n        right: %s\n", f.Left, f.Right)
			}
		}
	}
	switch {
	case len(results) == 0:
		fmt.Fprintln(w, "no tests to run")
	case failed == 0:
		fmt.Fprintf(w, "%d %s passed\n", len(results), plural(len(results), "test", "tests"))
	default:
		fmt.Fprintf(w, "%d %s, %d failed\n", len(results), plural(len(results), "test", "tests"), failed)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
