package gogen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
)

// TestFile is the generated file that holds the tests. go build skips it;
// vibe test runs it with go test.
const TestFile = "app_test.go"

// Tests returns every test of p in the order vibe test reports them: by
// feature, then as declared.
func Tests(p *ir.Program) []*ir.Test {
	var out []*ir.Test
	for _, ft := range p.Features {
		out = append(out, ft.Tests...)
	}
	return out
}

// TestFunc returns the name of the Go test that runs the i-th test of
// Tests, counting from 0.
func TestFunc(i int) string { return "TestVibe" + strconv.Itoa(i+1) }

const runTest = `// runVibeTest runs one test on a fresh app and prints its failure for
// vibe test.
func runVibeTest(t *testing.T, test func(*vibe.Tester, *vibe.Ctx) error) {
	if err := newApp().Test(t.Context(), test); err != nil {
		t.Fatal(vibe.Report(err))
	}
}
`

// tests returns app_test.go: a Go test per test block. Each one runs the
// test's code on a fresh app, with c for its queries and tc for its
// requests.
func (g *gen) tests() string {
	tests := Tests(g.prog)
	if len(tests) == 0 {
		return header
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString(runTest)
	for i, t := range tests {
		f := newBody(g)
		f.test = t
		f.markUsed(t.Body)
		f.block(t.Body)
		if !terminates(t.Body) {
			f.line("return nil")
		}
		name := TestFunc(i)
		code := "vibeTest" + strconv.Itoa(i+1)
		fmt.Fprintf(&b, "\n// %s runs the test %s of feature %s, declared at %s.\nfunc %s(t *testing.T) {\n\trunVibeTest(t, %s)\n}\n",
			name, strconv.Quote(t.Name), t.Feature, where(t.Pos), name, code)
		fmt.Fprintf(&b, "\n// %s is the code of the test %s.\nfunc %s(tc *vibe.Tester, c *vibe.Ctx) error {\n%s}\n",
			code, strconv.Quote(t.Name), code, f.w.String())
	}
	return b.String()
}

// request sends a request of a test and returns the response.
func (f *body) request(e *ir.Request) string {
	if f.test == nil {
		f.g.unsupported(e.Position(), "requests outside tests")
	}
	parts := make([]string, len(e.Path))
	for i, p := range e.Path {
		if k, ok := p.(*ir.Const); ok {
			if s, ok := k.Value.(string); ok {
				parts[i] = strconv.Quote(s)
				continue
			}
		}
		parts[i] = "vibe.PathSeg(" + f.expr(p) + ")"
	}
	args := []string{strconv.Quote(e.Method), strings.Join(parts, " + ")}
	for _, a := range e.Args {
		args = append(args, "vibe.With("+strconv.Quote(a.Name)+", "+f.expr(a.Value)+")")
	}
	t := f.temp()
	f.line("%s, err := tc.Do(%s)", t, strings.Join(args, ", "))
	f.failOn()
	return t
}

// member reads the status or body of a response, or a field of a JSON
// value, which fails the test when it is missing.
func (f *body) member(e *ir.Member) string {
	x := f.expr(e.X)
	if e.X.Type().Kind == ir.Response {
		if e.Name == "status" {
			return paren(x) + ".Status"
		}
		return paren(x) + ".Body"
	}
	t := f.temp()
	f.line("%s, err := %s.Field(%s)", t, paren(x), strconv.Quote(e.Name))
	f.failOn()
	return t
}

// loginAs starts a session for a user, which the requests that follow
// send.
func (f *body) loginAs(e *ir.Builtin) string {
	if f.test == nil || f.g.prog.Auth == nil {
		f.g.unsupported(e.Position(), "login_as outside tests")
	}
	u := f.value(e.Args[0], ir.ModelOf(f.g.prog.Auth))
	t := f.temp()
	f.line("%s, err := startSession(c, %s)", t, u)
	f.failOn()
	f.line("tc.LoginAs(%s)", t)
	return ""
}

// expect fails the test when its condition is false. A comparison
// reports the values of both sides; any other condition reports its
// value against true.
func (f *body) expect(s *ir.Expect) {
	file, line := strconv.Quote(s.Pos.File), s.Pos.Line
	if b, ok := s.Cond.(*ir.Binary); ok && isComparison(b.Op) {
		x, y := f.side(b.X), f.side(b.Y)
		cond := f.compareCode(b, x, y)
		f.line("if !%s {\nreturn vibe.ExpectFailed(%s, %d, %s, %s, %s)\n}", paren(cond), file, line, strconv.Quote(s.Text), x, y)
		return
	}
	cond := f.value(s.Cond, ir.Basic(ir.Bool))
	f.line("if !%s {\nreturn vibe.ExpectFailed(%s, %d, %s, false, true)\n}", paren(cond), file, line, strconv.Quote(s.Text))
}

// side generates one side of an expected comparison, so the failure can
// print the value it compared. A side that is more than a name or a
// field goes into a local first, so it runs once; values never change,
// so a name reads the same value twice.
func (f *body) side(e ir.Expr) string {
	x := f.expr(e)
	if _, ok := e.(*ir.Const); ok || selector.MatchString(x) {
		return x
	}
	t := f.temp()
	f.line("%s := %s", t, x)
	return t
}

// selector matches a Go name or a chain of fields, such as res.Status.
var selector = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

func isComparison(op string) bool {
	switch op {
	case "==", "!=", "<", "<=", ">", ">=", "contains":
		return true
	}
	return false
}
