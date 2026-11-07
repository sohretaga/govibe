package testrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/sohretaga/govibe/internal/build"
	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/gogen"
	"github.com/sohretaga/govibe/internal/ir"
	vibe "github.com/sohretaga/govibe/runtime"
)

// Options configures a test run.
type Options struct {
	Root string // the project root
	Go   string // the go command; "" looks it up on the PATH
	Run  string // a regular expression; only tests whose name matches run
}

// Result is the outcome of one test.
type Result struct {
	Name       string        `json:"name"`
	Passed     bool          `json:"passed"`
	DurationMS int64         `json:"duration_ms"`
	Failure    *vibe.Failure `json:"failure,omitempty"`
}

// Run builds the app of prog with its tests and runs the tests whose name
// matches opts.Run, in the order Tests lists them. Problems the user can
// act on, such as a compile error, are diagnostics; other failures, such
// as a malformed pattern, are errors.
func Run(ctx context.Context, prog *ir.Program, opts Options) ([]Result, []diag.Diagnostic, error) {
	match, err := regexp.Compile(opts.Run)
	if err != nil {
		return nil, nil, fmt.Errorf("-run %q: %w", opts.Run, err)
	}
	var selected []int
	var funcs []string
	tests := gogen.Tests(prog)
	for i, t := range tests {
		if match.MatchString(t.Name) {
			selected = append(selected, i)
			funcs = append(funcs, gogen.TestFunc(i))
		}
	}
	m, diags, err := build.Prepare(prog, build.Options{Root: opts.Root, Go: opts.Go})
	if m == nil || err != nil {
		return nil, diags, err
	}
	// Without a matching test, the run still compiles the app and tests.
	pattern := "^$"
	if len(funcs) > 0 {
		pattern = "^(" + strings.Join(funcs, "|") + ")$"
	}
	stdout, stderr, err := m.Run(ctx, "test", "-json", "-count=1", "-trimpath", "-run", pattern, ".")
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, nil, err
	}
	run := parse(stdout)
	if run.buildFailed || err != nil && len(run.tests) == 0 {
		return nil, build.MapErrors(opts.Root, prog, run.build.String()+string(stderr)), nil
	}
	results := make([]Result, len(selected))
	for k, i := range selected {
		results[k] = run.result(tests[i], funcs[k])
	}
	return results, nil, nil
}

// event is one line of go test -json output.
type event struct {
	Time    time.Time
	Action  string
	Test    string
	Output  string
	Elapsed float64
}

// testEvents is what go test reported about one test.
type testEvents struct {
	start, end time.Time
	action     string // pass, fail or skip; "" when the test did not finish
	elapsed    float64
	output     strings.Builder
}

// goRun is the parsed output of go test -json.
type goRun struct {
	tests       map[string]*testEvents
	build       strings.Builder // compiler output
	buildFailed bool
	output      strings.Builder // output outside any test, such as a crash
}

func parse(out []byte) *goRun {
	r := &goRun{tests: map[string]*testEvents{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e event
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			r.output.WriteString(sc.Text() + "\n")
			continue
		}
		switch {
		case e.Action == "build-output":
			r.build.WriteString(e.Output)
		case e.Action == "build-fail":
			r.buildFailed = true
		case e.Test == "":
			if e.Action == "output" {
				r.output.WriteString(e.Output)
			}
		default:
			t := r.tests[e.Test]
			if t == nil {
				t = &testEvents{}
				r.tests[e.Test] = t
			}
			switch e.Action {
			case "run":
				t.start = e.Time
			case "output":
				t.output.WriteString(e.Output)
			case "pass", "fail", "skip":
				t.action, t.end, t.elapsed = e.Action, e.Time, e.Elapsed
			}
		}
	}
	return r
}

// result returns the outcome of test t, which ran as the Go test fn.
func (r *goRun) result(t *ir.Test, fn string) Result {
	res := Result{Name: t.Name}
	ev := r.tests[fn]
	if ev == nil || ev.action == "" {
		out := r.output.String()
		if ev != nil {
			out = ev.output.String() + out
		}
		res.Failure = &vibe.Failure{File: t.Pos.File, Line: t.Pos.Line, Message: "the test did not finish: " + tail(out)}
		return res
	}
	res.DurationMS = ev.elapsedMS()
	if ev.action != "fail" {
		res.Passed = true
		return res
	}
	res.Failure = report(ev.output.String())
	if res.Failure == nil {
		res.Failure = &vibe.Failure{File: t.Pos.File, Line: t.Pos.Line, Message: tail(ev.output.String())}
	}
	return res
}

func (e *testEvents) elapsedMS() int64 {
	if !e.start.IsZero() && !e.end.IsZero() && e.end.After(e.start) {
		return e.end.Sub(e.start).Milliseconds()
	}
	return int64(e.elapsed * 1000)
}

// report returns the failure a test printed with vibe.Report, or nil.
func report(output string) *vibe.Failure {
	for _, line := range strings.Split(output, "\n") {
		_, data, ok := strings.Cut(line, vibe.FailurePrefix)
		if !ok {
			continue
		}
		var f vibe.Failure
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &f) == nil {
			return &f
		}
	}
	return nil
}

// maxTail is the most lines of raw output a failure without a report
// keeps.
const maxTail = 20

// tail returns the last lines of go test output, without the lines go
// test adds around each test.
func tail(out string) string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "=== ") || strings.HasPrefix(trimmed, "--- ") ||
			trimmed == "FAIL" || trimmed == "PASS" || strings.HasPrefix(trimmed, "FAIL\t") || strings.HasPrefix(trimmed, "ok \t") {
			continue
		}
		lines = append(lines, trimmed)
	}
	if len(lines) > maxTail {
		lines = lines[len(lines)-maxTail:]
	}
	if len(lines) == 0 {
		return "go test printed nothing"
	}
	return strings.Join(lines, "\n")
}
