package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/golden"
)

// TestCheckGolden runs "vibe check --json" on each file in testdata/check
// as a one-file project and compares the output with its .diag.json.
func TestCheckGolden(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/check/*.vibe")
	if err != nil || len(paths) < 20 {
		t.Fatalf("want at least 20 check cases, found %d: %v", len(paths), err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".vibe")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), src, 0o644); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := Check(context.Background(), dir, true, &stdout, &stderr)
			golden.Assert(t, "../../testdata/check/"+name+".diag.json", stdout.Bytes())

			var r result
			if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
				t.Fatalf("output is not one JSON object: %v", err)
			}
			if code != ExitFailure || r.OK || len(r.Diagnostics) == 0 {
				t.Fatalf("exit %d, ok %v, %d diagnostics; want a failure", code, r.OK, len(r.Diagnostics))
			}
			for _, d := range r.Diagnostics {
				if !strings.HasPrefix(string(d.Code), "V1") || d.Range.Start.Line < 1 || d.File == "" {
					t.Errorf("not a located V1xx diagnostic: %v", d)
				}
			}
		})
	}
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
