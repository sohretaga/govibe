package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/ast"
	"github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/parser"
	"github.com/sohretaga/govibe/internal/token"
)

// roundTrip formats src and checks the three properties of the formatter:
// the output parses, it is the same program with the same comments, and
// formatting it again changes nothing.
func roundTrip(t *testing.T, name string, src []byte) []byte {
	t.Helper()
	before, diags := parser.ParseFile(token.NewFile(name, src))
	if len(diags) > 0 {
		t.Fatalf("input does not parse: %v", diags)
	}
	out := File(before)
	after, diags := parser.ParseFile(token.NewFile(name, out))
	if len(diags) > 0 {
		t.Fatalf("output does not parse: %v\n%s", diags, out)
	}
	if a, b := ast.Dump(before), ast.Dump(after); a != b {
		t.Fatalf("formatting changed the tree:\n%s\noutput:\n%s", golden.Diff(a, b), out)
	}
	again, _ := Source(name, out)
	if string(again) != string(out) {
		t.Fatalf("formatting is not idempotent:\n%s", golden.Diff(string(out), string(again)))
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	paths, err := filepath.Glob("../../testdata/parse/*.vibe")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no cases: %v", err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".vibe")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out := roundTrip(t, name+".vibe", src)
			golden.Assert(t, "../../testdata/format/"+name+".vibe.golden", out)
		})
	}
}

// TestReferenceApp checks that the reference app from the spec is already
// in canonical form.
func TestReferenceApp(t *testing.T) {
	src, err := os.ReadFile("../../examples/library/main.vibe")
	if err != nil {
		t.Fatal(err)
	}
	if out := roundTrip(t, "main.vibe", src); string(out) != string(src) {
		t.Errorf("the reference app is not in canonical form:\n%s", golden.Diff(string(src), string(out)))
	}
}

func TestLayout(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"indentation and spacing",
			"model   Book{\n\ttitle :Text min 2   max 200\n}",
			"model Book {\n  title: Text min 2 max 200\n}\n"},
		{"one blank line between declarations",
			"\n\nauth User\nmodel A {}\n\n\n\nmodel B {}\n\n",
			"auth User\n\nmodel A {}\n\nmodel B {}\n"},
		{"blank lines between fields collapse",
			"model A {\n\n  x: Int\n\n\n  y: Int\n\n}",
			"model A {\n  x: Int\n\n  y: Int\n}\n"},
		{"one blank line between feature items",
			"feature F {\n  crud A at /a\n  get /b = 1\n\n\n  test \"t\" {\n  }\n}",
			"feature F {\n  crud A at /a\n\n  get /b = 1\n\n  test \"t\" {}\n}\n"},
		{"guard clauses stay on one line",
			"fn f(x: Int) -> Int {\n  if x > 1 {\n    return 1\n  }\n  if x < 0 { fail \"neg\" }\n  if x == 0 { let y = 1 }\n  return x\n}",
			"fn f(x: Int) -> Int {\n  if x > 1 { return 1 }\n  if x < 0 { fail \"neg\" }\n  if x == 0 {\n    let y = 1\n  }\n  return x\n}\n"},
		{"else chains",
			"fn f(x: Int) -> Int {\n  if x > 1 { return 1 } else if x > 0 { return 2 } else { return 3 }\n}",
			"fn f(x: Int) -> Int {\n  if x > 1 {\n    return 1\n  } else if x > 0 {\n    return 2\n  } else {\n    return 3\n  }\n}\n"},
		{"expressions join onto one line",
			"fn f() -> Int {\n  return g(\n    a: 1,\n    b: [\n      2,\n      3,\n    ],\n  ) +\n    4\n}",
			"fn f() -> Int {\n  return g(a: 1, b: [2, 3]) + 4\n}\n"},
		{"operators and literals",
			"fn f() -> Int {\n  return -a*(b+c)/d%e-not_x+ 1_000+7.50\n}",
			"fn f() -> Int {\n  return -a * (b + c) / d % e - not_x + 1_000 + 7.50\n}\n"},
		{"canonical string escapes",
			"fn f() -> Text {\n  return \"a\tb {x}\\{\"\n}",
			"fn f() -> Text {\n  return \"a\\tb {x}\\{\"\n}\n"},
		{"empty route parameters are dropped",
			"feature F {\n  get /a () = 1\n  post /b ( x : Int , ) { return x }\n}",
			"feature F {\n  get /a = 1\n\n  post /b (x: Int) {\n    return x\n  }\n}\n"},
		{"comments in expressions move above",
			"fn f() -> Int {\n  return g(\n    # why\n    a: 1, # first\n    b: 2,\n  ) # done\n}",
			"fn f() -> Int {\n  # why\n  # first\n  return g(a: 1, b: 2) # done\n}\n"},
		{"comments keep blocks open",
			"model A {\n  # later\n}\nfn f() -> Int {\n  if x { # check\n    return 1\n  }\n}",
			"model A {\n  # later\n}\n\nfn f() -> Int {\n  if x { # check\n    return 1\n  }\n}\n"},
		{"member of a request without arguments",
			"feature F {\n  test \"t\" {\n    let a = post / .a\n    let b = (get /x).status\n    let c = get /x ()(1)\n  }\n}",
			"feature F {\n  test \"t\" {\n    let a = post / .a\n    let b = (get /x).status\n    let c = get /x ()(1)\n  }\n}\n"},
		{"empty file", "\n\n", ""},
		{"only comments", "# a\n\n\n# b", "# a\n\n# b\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, diags := Source("main.vibe", []byte(tt.src))
			if len(diags) > 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if string(got) != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			if len(got) > 0 {
				roundTrip(t, "main.vibe", got)
			}
		})
	}
}

func TestSourceWithErrors(t *testing.T) {
	out, diags := Source("main.vibe", []byte("model A {\n  title Text\n}"))
	if out != nil || len(diags) == 0 {
		t.Errorf("Source = %q, %v; want no output and diagnostics", out, diags)
	}
}
