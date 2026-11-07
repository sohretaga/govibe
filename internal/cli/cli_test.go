package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/golden"
)

// checkCases returns the cases in testdata/check: a .vibe file is a
// one-file project, a folder a project of several files.
func checkCases(t *testing.T) map[string]map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir("../../testdata/check")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]map[string][]byte{}
	for _, ent := range entries {
		path := filepath.Join("../../testdata/check", ent.Name())
		switch {
		case ent.IsDir():
			files := map[string][]byte{}
			sub, err := os.ReadDir(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range sub {
				data, err := os.ReadFile(filepath.Join(path, f.Name()))
				if err != nil {
					t.Fatal(err)
				}
				files[f.Name()] = data
			}
			cases[ent.Name()] = files
		case strings.HasSuffix(ent.Name(), ".vibe"):
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cases[strings.TrimSuffix(ent.Name(), ".vibe")] = map[string][]byte{ent.Name(): data}
		}
	}
	return cases
}

// runCheck runs "vibe check --json" on a project made of files.
func runCheck(t *testing.T, files map[string][]byte) (result, []byte, int) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var stdout, stderr bytes.Buffer
	code := Check(context.Background(), dir, true, &stdout, &stderr)
	var r result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, stdout.String())
	}
	return r, stdout.Bytes(), code
}

// TestCheckGolden runs "vibe check --json" on each case in testdata/check
// and compares the output with its .diag.json file.
func TestCheckGolden(t *testing.T) {
	cases := checkCases(t)
	if len(cases) < 20 {
		t.Fatalf("want at least 20 check cases, found %d", len(cases))
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			r, out, code := runCheck(t, files)
			golden.Assert(t, "../../testdata/check/"+name+".diag.json", out)
			if code != ExitFailure || r.OK || len(r.Diagnostics) == 0 {
				t.Fatalf("exit %d, ok %v, %d diagnostics; want a failure", code, r.OK, len(r.Diagnostics))
			}
			for _, d := range r.Diagnostics {
				if d.Range.Start.Line < 1 || d.Message == "" {
					t.Errorf("diagnostic without location or message: %v", d)
				}
			}
		})
	}
}

// TestCodesHaveCases checks that every diagnostic code that the front
// end reports has a check case that triggers it. V9xx codes come from
// vibe build, which has its own tests.
func TestCodesHaveCases(t *testing.T) {
	seen := map[diag.Code]bool{}
	for name := range checkCases(t) {
		data, err := os.ReadFile("../../testdata/check/" + name + ".diag.json")
		if err != nil {
			t.Fatal(err)
		}
		var r result
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		for _, d := range r.Diagnostics {
			seen[d.Code] = true
		}
	}
	for _, info := range diag.All() {
		if !seen[info.Code] && !strings.HasPrefix(string(info.Code), "V9") {
			t.Errorf("%s (%s) has no case in testdata/check", info.Code, info.Title)
		}
	}
}

// TestFixesWork applies each fix of each check case and checks that the
// diagnostic is gone and that no new syntax error appeared.
func TestFixesWork(t *testing.T) {
	for name, files := range checkCases(t) {
		before, _, _ := runCheck(t, files)
		for i, d := range before.Diagnostics {
			for j, fix := range d.Fixes {
				t.Run(fmt.Sprintf("%s/%d/%d", name, i, j), func(t *testing.T) {
					fixed := map[string][]byte{}
					for n, data := range files {
						fixed[n] = data
					}
					fixed[d.File] = applyEdits(t, files[d.File], fix.Edits)
					after, _, _ := runCheck(t, fixed)
					for _, a := range after.Diagnostics {
						if a.Code == d.Code && a.Message == d.Message {
							t.Errorf("fix %q did not remove %s: %s\n%s", fix.Title, d.Code, d.Message, fixed[d.File])
						}
					}
					if syntaxErrors(after) > syntaxErrors(before) {
						t.Errorf("fix %q added a syntax error: %v\n%s", fix.Title, after.Diagnostics, fixed[d.File])
					}
				})
			}
		}
	}
}

func syntaxErrors(r result) int {
	n := 0
	for _, d := range r.Diagnostics {
		if strings.HasPrefix(string(d.Code), "V1") {
			n++
		}
	}
	return n
}

// applyEdits applies edits whose ranges count lines and code points.
func applyEdits(t *testing.T, src []byte, edits []diag.Edit) []byte {
	t.Helper()
	offset := func(p diag.Position) int {
		line, col, off := 1, 1, 0
		for _, r := range string(src) {
			if line == p.Line && col == p.Col {
				return off
			}
			if r == '\n' {
				line, col = line+1, 1
			} else {
				col++
			}
			off += utf8.RuneLen(r)
		}
		return off
	}
	sorted := slices.Clone(edits)
	slices.SortFunc(sorted, func(a, b diag.Edit) int { return offset(b.Range.Start) - offset(a.Range.Start) })
	out := string(src)
	for _, e := range sorted {
		start, end := offset(e.Range.Start), offset(e.Range.End)
		out = out[:start] + e.NewText + out[end:]
	}
	return []byte(out)
}

func makeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckText(t *testing.T) {
	dir := makeProject(t, map[string]string{"main.vibe": "model Book {\n  title Text\n}\n"})
	var stdout, stderr bytes.Buffer
	if code := Check(context.Background(), dir, false, &stdout, &stderr); code != ExitFailure {
		t.Errorf("exit %d, want %d", code, ExitFailure)
	}
	want := "main.vibe:2:9: error V101: expected ':' after field name 'title', found type name 'Text'\n" +
		"  2 |   title Text\n" +
		"    |         ^^^^\n" +
		"  fix: Insert ':'\n" +
		"  help: vibe explain V101\n"
	if stderr.String() != want || stdout.Len() > 0 {
		t.Errorf("stderr:\n%s\nwant:\n%s\nstdout: %q", stderr.String(), want, stdout.String())
	}
}

func TestCheckEmptyProject(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Check(context.Background(), t.TempDir(), true, &stdout, &stderr)
	if code != ExitFailure || !strings.Contains(stdout.String(), `"code": "V506"`) {
		t.Errorf("exit %d, output %s", code, stdout.String())
	}
}

func TestCheckMissingDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Check(context.Background(), filepath.Join(t.TempDir(), "nope"), true, &stdout, &stderr)
	var r result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || code != ExitFailure || r.OK || r.Error == "" {
		t.Errorf("exit %d, result %+v, err %v", code, r, err)
	}
}

func TestCheckReferenceApp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Check(context.Background(), "../../examples/library", true, &stdout, &stderr)
	if code != ExitOK || stdout.String() != "{\n  \"ok\": true,\n  \"diagnostics\": []\n}\n" {
		t.Errorf("exit %d, output %s %s", code, stdout.String(), stderr.String())
	}
}

func TestFmt(t *testing.T) {
	const messy = "model   Book{\n\ttitle :Text\n}\n"
	const clean = "model Book {\n  title: Text\n}\n"
	const broken = "model Book {\n  title Text\n}\n"
	dir := makeProject(t, map[string]string{"a.vibe": messy, "b.vibe": clean, "sub/c.vibe": broken})
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	var stdout, stderr bytes.Buffer
	code := Fmt(context.Background(), dir, true, true, &stdout, &stderr)
	var r result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if code != ExitFailure || r.OK || len(r.Files) != 1 || r.Files[0] != "a.vibe" || len(r.Diagnostics) != 1 || r.Diagnostics[0].File != "sub/c.vibe" {
		t.Errorf("fmt --check --json: exit %d, %+v", code, r)
	}
	if read("a.vibe") != messy {
		t.Error("fmt --check rewrote a file")
	}

	stdout.Reset()
	stderr.Reset()
	if code := Fmt(context.Background(), dir, false, false, &stdout, &stderr); code != ExitFailure {
		t.Errorf("fmt with a broken file: exit %d", code)
	}
	if read("a.vibe") != clean || read("b.vibe") != clean || read("sub/c.vibe") != broken {
		t.Errorf("fmt: a=%q b=%q c=%q", read("a.vibe"), read("b.vibe"), read("sub/c.vibe"))
	}
	if stdout.String() != "a.vibe\n" || !strings.Contains(stderr.String(), "sub/c.vibe:2:9: error V101") {
		t.Errorf("fmt output: stdout %q stderr %q", stdout.String(), stderr.String())
	}

	if err := os.Remove(filepath.Join(dir, "sub", "c.vibe")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := Fmt(context.Background(), dir, true, false, &stdout, &stderr); code != ExitOK || stdout.Len() > 0 {
		t.Errorf("fmt --check on a formatted project: exit %d, stdout %q", code, stdout.String())
	}
}

func TestFmtExamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Fmt(context.Background(), "../../examples/library", true, false, &stdout, &stderr); code != ExitOK {
		t.Errorf("examples/library is not formatted: %s%s", stdout.String(), stderr.String())
	}
}

func TestRoutesGolden(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Routes(context.Background(), "../../examples/library", false, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	golden.Assert(t, "testdata/library.routes.golden", stdout.Bytes())
	stdout.Reset()
	if code := Routes(context.Background(), "../../examples/library", true, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	golden.Assert(t, "testdata/library.routes.json", stdout.Bytes())
}

func TestRoutesWithErrors(t *testing.T) {
	dir := makeProject(t, map[string]string{"main.vibe": "model Book {\n  title: Text\n}\n"})
	var stdout, stderr bytes.Buffer
	code := Routes(context.Background(), dir, true, &stdout, &stderr)
	var r result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || code != ExitFailure || r.OK || r.Routes != nil {
		t.Errorf("exit %d, %+v, %v", code, r, err)
	}
}

func TestExplain(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Explain("v201", false, &stdout, &stderr); code != ExitOK || !strings.HasPrefix(stdout.String(), "V201: unknown field\n") || !strings.Contains(stdout.String(), "Before:") {
		t.Errorf("explain v201: %d %q", code, stdout.String())
	}
	stdout.Reset()
	if code := Explain("V999", true, &stdout, &stderr); code != ExitFailure || !strings.Contains(stdout.String(), `"ok": false`) {
		t.Errorf("explain V999: %d %q", code, stdout.String())
	}
	for _, info := range diag.All() {
		stdout.Reset()
		if code := Explain(string(info.Code), true, &stdout, &stderr); code != ExitOK {
			t.Errorf("explain %s: exit %d", info.Code, code)
		}
	}
}

// TestCheckSpeed checks the spec's target: vibe check on 5,000 lines in
// under 100 ms. It runs the reference app copied into many files.
func TestCheckSpeed(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing needs a normal build")
	}
	src, err := os.ReadFile("../../examples/library/main.vibe")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"main.vibe": string(src)}
	lines := strings.Count(string(src), "\n")
	body := string(src[strings.Index(string(src), "model Book"):])
	for i := 0; lines < 5000; i++ {
		n := fmt.Sprint(i)
		renamed := strings.NewReplacer("model Book", "model Book"+n, "feature Books", "feature Books"+n,
			"Book.", "Book"+n+".", "crud Book at /books", "crud Book"+n+" at /books"+n,
			"/books/", "/books"+n+"/", "/books (", "/books"+n+" (").Replace(body)
		files["f"+n+".vibe"] = renamed
		lines += strings.Count(renamed, "\n")
	}
	dir := makeProject(t, files)
	var stdout, stderr bytes.Buffer
	start := time.Now()
	code := Check(context.Background(), dir, true, &stdout, &stderr)
	elapsed := time.Since(start)
	if code != ExitOK {
		t.Fatalf("the generated project does not check: %s", stdout.String())
	}
	t.Logf("%d lines in %d files checked in %v", lines, len(files), elapsed)
	if elapsed > 100*time.Millisecond {
		t.Errorf("vibe check took %v for %d lines, want under 100ms", elapsed, lines)
	}
}

func TestBuildWithErrors(t *testing.T) {
	dir := makeProject(t, map[string]string{"main.vibe": "app A {}\n\nmodel Book {\n  title: Txt\n}\n"})
	var stdout, stderr bytes.Buffer
	code := Build(context.Background(), dir, "", true, &stdout, &stderr)
	var r result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || code != ExitFailure || r.OK || len(r.Diagnostics) == 0 || r.Output != "" {
		t.Fatalf("exit %d, result %+v, err %v", code, r, err)
	}
	if r.Diagnostics[0].Code != diag.V101 {
		t.Errorf("first diagnostic = %+v, want V101", r.Diagnostics[0])
	}
	if _, err := os.Stat(filepath.Join(dir, ".vibe")); err == nil {
		t.Error("a project with errors must not get a build directory")
	}
}

func TestBuildUnsupported(t *testing.T) {
	dir := makeProject(t, map[string]string{"main.vibe": "app A {}\nauth User\n\nmodel User {\n  email: Email unique\n}\n"})
	var stdout, stderr bytes.Buffer
	code := Build(context.Background(), dir, "", false, &stdout, &stderr)
	if code != ExitFailure || !strings.Contains(stderr.String(), "main.vibe:4:1: vibe build does not support auth yet") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}

func TestBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped with -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command on the PATH")
	}
	dir := makeProject(t, map[string]string{"main.vibe": "app BookShop {}\n\nmodel Book {\n  title: Text\n}\n\nfeature Books {\n  crud Book at /books\n}\n"})
	var stdout, stderr bytes.Buffer
	code := Build(context.Background(), dir, "", true, &stdout, &stderr)
	var r result
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || code != ExitOK || !r.OK {
		t.Fatalf("exit %d, result %+v, err %v, stderr %s", code, r, err, stderr.String())
	}
	name := "book_shop"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if want := "." + string(filepath.Separator) + name; r.Output != want {
		t.Errorf("output = %q, want %q", r.Output, want)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Errorf("no binary: %v", err)
	}
}
