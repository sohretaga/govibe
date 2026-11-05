package parser

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/token"
)

// brief renders a diagnostic as "code start-end [fix texts]".
func brief(d diag.Diagnostic) string {
	var fixes []string
	for _, f := range d.Fixes {
		for _, e := range f.Edits {
			fixes = append(fixes, strconv.Quote(e.NewText))
		}
	}
	return fmt.Sprintf("%s %d:%d-%d:%d [%s]", d.Code,
		d.Range.Start.Line, d.Range.Start.Col, d.Range.End.Line, d.Range.End.Col, strings.Join(fixes, " "))
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"missing colon", "model Book {\n  title Text\n}", `V101 2:9-2:13 [":"]`},
		{"keyword as field name", "model Book {\n  owner: Ref User\n}", `V101 2:3-2:8 []`},
		{"misspelled field type", "model A {\n  x: Txt\n}", `V101 2:6-2:9 ["Text" "Ref "]`},
		{"model type without Ref", "model A {\n  u: User\n}", `V101 2:6-2:10 ["Ref "]`},
		{"field and brace on one line", "model A {\n  x: Int }", `V101 2:10-2:11 ["\n"]`},
		{"snake_case model name", "model book_review {}", `V108 1:7-1:18 ["BookReview"]`},
		{"camelCase field name", "model A {\n  createdBy: Time\n}", `V108 2:3-2:12 ["created_by"]`},
		{"test outside a feature", `test "x" {}`, `V101 1:1-1:5 []`},
		{"func instead of fn", "func f() -> Int { return 1 }", `V101 1:1-1:5 ["fn"]`},
		{"misspelled keyword", "modle Book {}", `V101 1:1-1:6 ["model"]`},
		{"unknown crud op", "feature F {\n  crud A at /a only list, remove\n}", `V101 2:27-2:33 ["delete"]`},
		{"every unknown crud op is reported", "feature F {\n  crud A at /a only read, list, remove\n}", "V101 2:21-2:25 [\"get\"]\nV101 2:33-2:39 [\"delete\"]"},
		{"crud without at", "feature F {\n  crud A /a\n}", `V101 2:10-2:11 ["at "]`},
		{"private without owner", "feature F {\n  crud A at /a private\n}", `V101 2:16-2:23 []`},
		{"route without body", "feature F {\n  get /a\n}", `V101 2:9-2:9 []`},
		{"else on its own line", "feature F {\n  post /a {\n    if x {\n      return 1\n    }\n    else {\n      return 2\n    }\n  }\n}", `V101 6:5-6:9 [" "]`},
		{"assignment in if condition", "feature F {\n  post /a {\n    if x = 5 { return 1 }\n  }\n}", `V101 3:10-3:11 ["=="]`},
		{"comparison in let", "fn f() -> Int {\n  let x == 5\n  return x\n}", `V101 2:9-2:11 ["="]`},
		{"chained comparison", "fn f(a: Int) -> Bool {\n  return 0 < a < 10\n}", `V105 2:10-2:20 ["0 < a and a < 10"]`},
		{"mixed arguments", "fn f() -> Int {\n  return g(1, b: 2)\n}", `V107 2:15-2:19 []`},
		{"misspelled status", "fn f() -> Int {\n  fail confict \"x\"\n}", `V101 2:8-2:15 ["conflict"]`},
		{"missing brace at end of file", "model A {\n  x: Int\n", `V101 3:1-3:1 ["}\n"]`},
		{"missing brace before next model", "model A {\n  x: Int\n\nmodel B {}", `V101 4:1-4:6 []`},
		{"path without slash", "feature F {\n  get books = 1\n}", `V101 2:7-2:12 ["/"]`},
		{"not inside a comparison", "fn f() -> Bool {\n  return a == not b\n}", `V101 2:15-2:20 ["(" ")"]`},
		{"extern inside a feature", "feature F {\n  extern fn x() -> Int\n}", `V101 2:3-2:9 []`},
		{"extern with a body", "extern fn x() -> Int { return 1 }", `V101 1:22-1:23 []`},
		{"missing arrow", "fn f() Int {\n  return 1\n}", `V101 1:8-1:11 ["-> "]`},
		{"expression as path parameter", "feature F {\n  get /a/{a.b} = 1\n}", `V101 2:10-2:15 []`},
		{"empty interpolation", "fn f() -> Text {\n  return \"{}\"\n}", `V101 2:11-2:13 []`},
		{"two statements on a line", "fn f() -> Int {\n  let a = 1 let b = 2\n}", `V101 2:13-2:16 []`},
		{"optional slug", "model A {\n  t: Text\n  s: Slug(t)?\n}", `V101 3:13-3:14 [""]`},
		{"list field", "model A {\n  tags: [Text]\n}", `V101 2:9-2:15 []`},
		{"stray closing brace", "}\nauth User", `V101 1:1-1:2 []`},
		{"put instead of patch", "feature F {\n  put /a = 1\n}", `V101 2:3-2:6 ["patch"]`},
		{"PascalCase member", "fn f() -> Int {\n  return a.Name\n}", `V108 2:12-2:16 ["name"]`},
		{"typed let", "fn f() -> Int {\n  let x: Int = 5\n}", `V101 2:8-2:9 []`},
		{"negative min", "model A {\n  x: Int min -1\n}", `V101 2:14-2:15 []`},
		{"unclosed list", "fn f() -> Int {\n  return [1, 2\n}", `V101 3:1-3:2 []`},
		{"unknown app setting", "app A {\n  port 8080\n}", `V101 2:3-2:7 []`},
		{"else without block", "fn f() -> Int {\n  if x {\n    return 1\n  } else return 2\n}", `V101 4:10-4:16 []`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, diags := ParseFile(token.NewFile("main.vibe", []byte(tt.src)))
			var got []string
			for _, d := range diags {
				got = append(got, brief(d))
			}
			if strings.Join(got, "\n") != tt.want {
				var msgs []string
				for _, d := range diags {
					msgs = append(msgs, d.Message)
				}
				t.Errorf("got:  %s\nwant: %s\nmessages: %q", strings.Join(got, "\n      "), tt.want, msgs)
			}
		})
	}
}
