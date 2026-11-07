package testrun

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/project"
	vibe "github.com/sohretaga/govibe/runtime"
)

// goTool returns the go command, or skips the test: a test run compiles
// the app.
func goTool(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds apps; skipped with -short")
	}
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command on the PATH")
	}
	return path
}

// load copies the project in src to a temporary root and checks it.
func load(t *testing.T, src string) (string, *ir.Program) {
	t.Helper()
	root := t.TempDir()
	srcs, err := project.Read(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs {
		if err := os.WriteFile(filepath.Join(root, s.Name), s.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prog, diags := project.Check(srcs)
	for _, d := range diags {
		t.Errorf("check: %v", d)
	}
	if prog == nil {
		t.FailNow()
	}
	return root, prog
}

func run(t *testing.T, goCmd, src, pattern string) []Result {
	t.Helper()
	root, prog := load(t, src)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	results, diags, err := Run(ctx, prog, Options{Root: root, Go: goCmd, Run: pattern})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		t.Fatalf("build: %s:%d: %s %s", d.File, d.Range.Start.Line, d.Code, d.Message)
	}
	return results
}

// TestReferenceTests runs the three tests of the reference app.
func TestReferenceTests(t *testing.T) {
	goCmd := goTool(t)
	results := run(t, goCmd, "../../examples/library", "")
	if len(results) != 3 {
		t.Fatalf("ran %d tests, want 3", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("%q failed: %+v", r.Name, r.Failure)
		}
	}
}

// TestFailures runs testdata/e2e/tests, whose tests fail in every way a
// test can, and compares the results, without durations, with the golden
// file.
func TestFailures(t *testing.T) {
	goCmd := goTool(t)
	results := run(t, goCmd, "../../testdata/e2e/tests", "")
	for i := range results {
		results[i].DurationMS = 0
	}
	got, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden.Assert(t, "../../testdata/e2e/tests/results.golden.json", append(got, '\n'))

	results = run(t, goCmd, "../../testdata/e2e/tests", "broken|missing")
	if len(results) != 2 || results[0].Name != "a broken expectation" || results[1].Name != "a record that is missing" {
		t.Errorf("-run broken|missing ran %+v", results)
	}
	if results := run(t, goCmd, "../../testdata/e2e/tests", "nothing matches"); len(results) != 0 {
		t.Errorf("-run with no match ran %+v", results)
	}
}

func TestBadPattern(t *testing.T) {
	_, prog := load(t, "../../examples/library")
	if _, _, err := Run(context.Background(), prog, Options{Root: t.TempDir(), Run: "("}); err == nil || !strings.Contains(err.Error(), "-run") {
		t.Errorf("Run with a bad pattern = %v", err)
	}
}

func events(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestParse(t *testing.T) {
	test := &ir.Test{Name: "a test", Pos: ir.Pos{File: "main.vibe", Line: 7}}
	report := vibe.Report(vibe.ExpectFailed("main.vibe", 9, "x == 1", int64(2), int64(1)))
	out, _ := json.Marshal(report)
	r := parse(events(
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"start","Package":"vibeapp"}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"run","Test":"TestVibe1"}`,
		`{"Time":"2026-01-01T00:00:00.001Z","Action":"output","Test":"TestVibe1","Output":"=== RUN   TestVibe1\n"}`,
		`{"Time":"2026-01-01T00:00:00.002Z","Action":"output","Test":"TestVibe1","Output":"    app_test.go:18: `+strings.Trim(string(out), `"`)+`\n"}`,
		`{"Time":"2026-01-01T00:00:00.009Z","Action":"fail","Test":"TestVibe1","Elapsed":0.01}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"run","Test":"TestVibe2"}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"pass","Test":"TestVibe2","Elapsed":0.02}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"run","Test":"TestVibe3"}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"output","Test":"TestVibe3","Output":"--- FAIL: TestVibe3 (0.00s)\n"}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"output","Test":"TestVibe3","Output":"    something odd\n"}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"fail","Test":"TestVibe3"}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"run","Test":"TestVibe4"}`,
		`{"Time":"2026-01-01T00:00:00.000Z","Action":"output","Package":"vibeapp","Output":"panic: test timed out after 10m0s\n"}`,
		`not json`,
	))
	if r.buildFailed {
		t.Error("buildFailed without a build-fail event")
	}

	got := r.result(test, "TestVibe1")
	want := vibe.Failure{File: "main.vibe", Line: 9, Message: "expect x == 1", Left: "2", Right: "1"}
	if got.Passed || got.DurationMS != 9 || got.Failure == nil || *got.Failure != want {
		t.Errorf("TestVibe1 = %+v %+v", got, got.Failure)
	}
	if got := r.result(test, "TestVibe2"); !got.Passed || got.DurationMS != 20 || got.Failure != nil {
		t.Errorf("TestVibe2 = %+v", got)
	}
	got = r.result(test, "TestVibe3")
	if got.Passed || got.Failure == nil || got.Failure.Line != 7 || got.Failure.Message != "something odd" {
		t.Errorf("TestVibe3 = %+v %+v", got, got.Failure)
	}
	for _, fn := range []string{"TestVibe4", "TestVibe5"} {
		got = r.result(test, fn)
		if got.Passed || got.Failure == nil || got.Failure.Message != "the test did not finish: panic: test timed out after 10m0s\nnot json" {
			t.Errorf("%s = %+v %+v", fn, got, got.Failure)
		}
	}

	b := parse(events(
		`{"ImportPath":"vibeapp [vibeapp.test]","Action":"build-output","Output":"# vibeapp\n"}`,
		`{"ImportPath":"vibeapp [vibeapp.test]","Action":"build-output","Output":"main.vibe:3:4: undefined: x\n"}`,
		`{"ImportPath":"vibeapp [vibeapp.test]","Action":"build-fail"}`,
	))
	if !b.buildFailed || b.build.String() != "# vibeapp\nmain.vibe:3:4: undefined: x\n" {
		t.Errorf("build output = %v %q", b.buildFailed, b.build.String())
	}
}

func TestTail(t *testing.T) {
	var lines []string
	for i := range maxTail + 5 {
		lines = append(lines, "line "+string(rune('a'+i)))
	}
	got := tail("=== RUN   TestVibe1\n" + strings.Join(lines, "\n") + "\n--- FAIL: TestVibe1\nFAIL\n")
	if n := strings.Count(got, "\n") + 1; n != maxTail || !strings.HasSuffix(got, lines[len(lines)-1]) {
		t.Errorf("tail kept %d lines: %q", n, got)
	}
	if got := tail("PASS\n"); got != "go test printed nothing" {
		t.Errorf("tail of nothing = %q", got)
	}
}
