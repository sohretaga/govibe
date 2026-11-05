package diag

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/sohretaga/govibe/internal/token"
)

// Limit is the maximum number of diagnostics reported in one run.
const Limit = 50

// Severity tells whether a diagnostic stops the build.
type Severity string

// Severities.
const (
	SeverityError Severity = "error"
)

// Position is a 1-based line and column; columns count Unicode code points.
type Position struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

// Range is a half-open source range: End is the first position after it.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Edit replaces the text in Range with NewText.
type Edit struct {
	Range   Range  `json:"range"`
	NewText string `json:"new_text"`
}

// Fix is a mechanical change that resolves a diagnostic.
type Fix struct {
	Title string `json:"title"`
	Edits []Edit `json:"edits"`
}

// Diagnostic is one reported problem.
type Diagnostic struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	File     string   `json:"file"`
	Range    Range    `json:"range"`
	Snippet  string   `json:"snippet"`
	Fixes    []Fix    `json:"fixes"`
	Help     string   `json:"help"`
}

// New returns an error diagnostic for code covering [start, end) in f. The
// message is the code's template formatted with args.
func New(f *token.File, code Code, start, end token.Pos, args ...any) Diagnostic {
	if end < start {
		end = start
	}
	r := RangeOf(f, start, end)
	return Diagnostic{
		Code:     code,
		Severity: SeverityError,
		Message:  code.Format(args...),
		File:     f.Name(),
		Range:    r,
		Snippet:  f.LineText(r.Start.Line),
		Fixes:    []Fix{},
		Help:     "vibe explain " + string(code),
	}
}

// WithFix returns d with a fix appended.
func (d Diagnostic) WithFix(title string, edits ...Edit) Diagnostic {
	d.Fixes = append(slices.Clip(d.Fixes), Fix{Title: title, Edits: edits})
	return d
}

// RangeOf converts [start, end) in f to a Range.
func RangeOf(f *token.File, start, end token.Pos) Range {
	s, e := f.Position(start), f.Position(end)
	return Range{Start: Position(s), End: Position(e)}
}

// Replace returns an edit that replaces [start, end) in f with text.
func Replace(f *token.File, start, end token.Pos, text string) Edit {
	return Edit{Range: RangeOf(f, start, end), NewText: text}
}

// Insert returns an edit that inserts text at p in f.
func Insert(f *token.File, p token.Pos, text string) Edit {
	return Replace(f, p, p, text)
}

// String formats d as "file:line:col: code: message".
func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d:%d: %s: %s", d.File, d.Range.Start.Line, d.Range.Start.Col, d.Code, d.Message)
}

// Sort orders diagnostics by file, position and code.
func Sort(ds []Diagnostic) {
	slices.SortStableFunc(ds, func(a, b Diagnostic) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Range.Start.Line, b.Range.Start.Line),
			cmp.Compare(a.Range.Start.Col, b.Range.Start.Col),
			cmp.Compare(a.Code, b.Code),
		)
	})
}

// Truncate sorts ds and keeps at most Limit diagnostics.
func Truncate(ds []Diagnostic) []Diagnostic {
	Sort(ds)
	if len(ds) > Limit {
		ds = ds[:Limit]
	}
	return ds
}

// HasErrors reports whether any diagnostic is an error.
func HasErrors(ds []Diagnostic) bool {
	return slices.ContainsFunc(ds, func(d Diagnostic) bool { return d.Severity == SeverityError })
}
