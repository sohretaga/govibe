package diag

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Report is the JSON envelope that "vibe check --json" prints.
type Report struct {
	OK          bool         `json:"ok"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// NewReport returns the envelope for ds. A nil ds becomes an empty list so
// that the JSON always holds an array.
func NewReport(ds []Diagnostic) Report {
	if ds == nil {
		ds = []Diagnostic{}
	}
	return Report{OK: !HasErrors(ds), Diagnostics: ds}
}

// WriteJSON writes v as indented JSON followed by a newline. HTML
// characters are not escaped, so messages keep their '<' and '&'.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

// WriteText writes ds for a human reader: the location and message, the
// source line with the range underlined, fixes and the explain command.
func WriteText(w io.Writer, ds []Diagnostic) error {
	var b strings.Builder
	for _, d := range ds {
		if d.File != "" {
			fmt.Fprintf(&b, "%s:%d:%d: ", d.File, d.Range.Start.Line, d.Range.Start.Col)
		}
		fmt.Fprintf(&b, "%s %s: %s\n", d.Severity, d.Code, d.Message)
		if d.Range.Start.Line > 0 && d.Snippet != "" {
			gutter := fmt.Sprintf("%d", d.Range.Start.Line)
			pad := strings.Repeat(" ", len(gutter))
			fmt.Fprintf(&b, "  %s | %s\n", gutter, d.Snippet)
			fmt.Fprintf(&b, "  %s | %s\n", pad, underline(d.Snippet, d.Range))
		}
		for _, f := range d.Fixes {
			fmt.Fprintf(&b, "  fix: %s\n", f.Title)
		}
		fmt.Fprintf(&b, "  help: %s\n", d.Help)
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write diagnostics: %w", err)
	}
	return nil
}

// underline returns carets under the part of line covered by r, keeping
// tabs so the carets line up with the source.
func underline(line string, r Range) string {
	start := r.Start.Col - 1
	end := r.End.Col - 1
	if r.End.Line != r.Start.Line {
		end = utf8.RuneCountInString(line)
	}
	if end <= start {
		end = start + 1
	}
	var b strings.Builder
	i := 0
	for _, c := range line {
		if i >= start {
			break
		}
		if c == '\t' {
			b.WriteByte('\t')
		} else {
			b.WriteByte(' ')
		}
		i++
	}
	for ; i < start; i++ {
		b.WriteByte(' ')
	}
	b.WriteString(strings.Repeat("^", end-start))
	return b.String()
}
