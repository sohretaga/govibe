package gogen

import (
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
)

// Generated code marks the statements that come from a .vibe statement:
// a line "//vibe:at file:line:col" before them and "//vibe:end" after.
// lineDirectives turns the marks into /*line*/ directives, so Go errors,
// panics and stack traces name .vibe lines.
const (
	markAt  = "//vibe:at "
	markEnd = "//vibe:end"
)

// mark writes the start of the code of a .vibe statement at p.
func (f *body) mark(p ir.Pos) {
	if p.Line > 0 {
		f.line("%s%s:%d:%d", markAt, p.File, p.Line, p.Col)
	}
}

// unmark writes the end of the code a mark started.
func (f *body) unmark(p ir.Pos) {
	if p.Line > 0 {
		f.line("%s", markEnd)
	}
}

// lineDirectives replaces the marks in a formatted file. Every line of a
// marked statement gets /*line file:line:col*/ before its code; the first
// line after one gets a directive back to its own place in the Go file.
// A line that starts with } gets none, nor does a line outside a
// function, since gofmt would move the directive; neither has code that
// can fail.
func lineDirectives(name string, src []byte) []byte {
	if !strings.Contains(string(src), markAt) {
		return src
	}
	var out []string
	var stack []string
	mapped := false
	for _, line := range strings.Split(string(src), "\n") {
		code := strings.TrimLeft(line, "\t")
		indent := line[:len(line)-len(code)]
		switch {
		case strings.HasPrefix(code, markAt):
			stack = append(stack, strings.TrimPrefix(code, markAt))
			continue
		case code == markEnd:
			stack = stack[:len(stack)-1]
			continue
		case code == "" || strings.HasPrefix(code, "}") || strings.HasPrefix(code, "//"):
		case len(stack) > 0:
			line = indent + directive(stack[len(stack)-1]) + code
			mapped = true
		case mapped && indent != "":
			// Directives count lines from themselves, so this one names the
			// line it is on in the final file.
			line = indent + directive(name+":"+strconv.Itoa(len(out)+1)+":"+strconv.Itoa(len(indent)+1)) + code
			mapped = false
		}
		out = append(out, line)
	}
	return []byte(strings.Join(out, "\n"))
}

// directive returns a /*line*/ comment and the space after it, which
// gofmt keeps, so that the code after the space is at the position pos.
func directive(pos string) string {
	i := strings.LastIndexByte(pos, ':')
	col, err := strconv.Atoi(pos[i+1:])
	if err != nil || col < 2 {
		return "/*line " + pos + "*/ "
	}
	return "/*line " + pos[:i+1] + strconv.Itoa(col-1) + "*/ "
}
