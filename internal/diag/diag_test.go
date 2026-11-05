package diag

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/token"
)

const source = "model Book {\n  title Text\n}\n"

func TestNew(t *testing.T) {
	f := token.NewFile("main.vibe", []byte(source))
	start := token.PosAt(strings.Index(source, "Text"))
	d := New(f, V101, start, start+4, "':'", "type name 'Text'").
		WithFix("Insert ':'", Insert(f, start-1, ":"))

	if d.Message != "expected ':', found type name 'Text'" {
		t.Errorf("Message = %q", d.Message)
	}
	want := Range{Start: Position{2, 9}, End: Position{2, 13}}
	if d.Range != want {
		t.Errorf("Range = %+v, want %+v", d.Range, want)
	}
	if d.Snippet != "  title Text" {
		t.Errorf("Snippet = %q", d.Snippet)
	}
	if d.Help != "vibe explain V101" || d.File != "main.vibe" || d.Severity != SeverityError {
		t.Errorf("unexpected header fields: %+v", d)
	}
	if len(d.Fixes) != 1 || d.Fixes[0].Edits[0].Range.Start != (Position{2, 8}) {
		t.Errorf("Fixes = %+v", d.Fixes)
	}
}

func TestTruncate(t *testing.T) {
	var ds []Diagnostic
	for i := range Limit + 10 {
		ds = append(ds, Diagnostic{File: "b.vibe", Range: Range{Start: Position{Limit + 10 - i, 1}}})
	}
	ds = append(ds, Diagnostic{File: "a.vibe", Range: Range{Start: Position{99, 1}}})
	got := Truncate(ds)
	if len(got) != Limit {
		t.Fatalf("len = %d, want %d", len(got), Limit)
	}
	if got[0].File != "a.vibe" || got[1].Range.Start.Line != 1 || got[Limit-1].Range.Start.Line != Limit-1 {
		t.Errorf("not sorted by file then line: first %v, second %v, last %v", got[0], got[1], got[Limit-1])
	}
}

func TestWriteText(t *testing.T) {
	f := token.NewFile("main.vibe", []byte("model Book {\n\ttitle Text\n}\n"))
	start := token.PosAt(strings.Index(string(f.Source()), "Text"))
	d := New(f, V101, start, start+4, "':'", "type name 'Text'").
		WithFix("Insert ':'", Insert(f, start-1, ":"))
	var b bytes.Buffer
	if err := WriteText(&b, []Diagnostic{d}); err != nil {
		t.Fatal(err)
	}
	want := "main.vibe:2:8: error V101: expected ':', found type name 'Text'\n" +
		"  2 | \ttitle Text\n" +
		"    | \t      ^^^^\n" +
		"  fix: Insert ':'\n" +
		"  help: vibe explain V101\n"
	if b.String() != want {
		t.Errorf("WriteText:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestWriteJSON(t *testing.T) {
	f := token.NewFile("main.vibe", []byte(source))
	start := token.PosAt(strings.Index(source, "Text"))
	d := New(f, V101, start, start+4, "':'", "type name 'Text'").
		WithFix("Insert ':'", Insert(f, start-1, ":"))
	var b bytes.Buffer
	if err := WriteJSON(&b, NewReport([]Diagnostic{d})); err != nil {
		t.Fatal(err)
	}
	golden.Assert(t, "testdata/report.json", b.Bytes())

	b.Reset()
	if err := WriteJSON(&b, NewReport(nil)); err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"ok\": true,\n  \"diagnostics\": []\n}\n"; b.String() != want {
		t.Errorf("empty report = %q, want %q", b.String(), want)
	}
}

func TestCodes(t *testing.T) {
	format := regexp.MustCompile(`^V[1-59][0-9][0-9]$`)
	seen := map[Code]bool{}
	for _, info := range All() {
		if !format.MatchString(string(info.Code)) {
			t.Errorf("%s: code does not match %s", info.Code, format)
		}
		if seen[info.Code] {
			t.Errorf("%s: declared twice", info.Code)
		}
		seen[info.Code] = true
		if info.Title == "" || info.Explanation == "" || info.Before == "" || info.After == "" {
			t.Errorf("%s: title, explanation and example are required", info.Code)
		}
		if !strings.Contains(info.Template, "%") {
			t.Errorf("%s: template %q names nothing concrete", info.Code, info.Template)
		}
		args := make([]any, strings.Count(info.Template, "%s"))
		for i := range args {
			args[i] = "x"
		}
		if msg := info.Code.Format(args...); strings.Contains(msg, "%!") {
			t.Errorf("%s: template %q is not a plain %%s template: %s", info.Code, info.Template, msg)
		}
	}
}

func TestSuggest(t *testing.T) {
	ops := []string{"list", "get", "create", "update", "delete"}
	tests := []struct {
		word       string
		candidates []string
		want       string
	}{
		{"titel", []string{"title", "pages"}, "title"},
		{"modle", []string{"app", "model", "feature"}, "model"},
		{"lst", ops, "list"},
		{"remove", ops, ""},
		{"crate", ops, "create"},
		{"x", []string{"or", "id"}, ""},
		{"list", ops, ""},
	}
	for _, tt := range tests {
		got, ok := Suggest(tt.word, tt.candidates)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("Suggest(%q) = %q, %v; want %q", tt.word, got, ok, tt.want)
		}
	}
}

func ExampleCode_Format() {
	fmt.Println(V105.Format("'a < b < c'"))
	// Output: comparison 'a < b < c' cannot be chained
}
