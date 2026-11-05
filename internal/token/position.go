package token

import (
	"sort"
	"unicode/utf8"
)

// Pos is a byte offset into a source file plus one, so that the zero value
// means "no position".
type Pos int

// NoPos is the zero Pos. It marks an absent token, such as an optional
// keyword that was not written.
const NoPos Pos = 0

// PosAt returns the Pos of the byte at offset.
func PosAt(offset int) Pos { return Pos(offset + 1) }

// IsValid reports whether p is a real position.
func (p Pos) IsValid() bool { return p > NoPos }

// Offset returns the 0-based byte offset of p.
func (p Pos) Offset() int { return int(p) - 1 }

// Position is a 1-based line and column. Columns count Unicode code points.
type Position struct {
	Line int
	Col  int
}

// File is the source text of one .vibe file with a line index.
type File struct {
	name  string
	src   []byte
	lines []int // byte offset where each line starts
}

// NewFile returns a File for src. The name is used in diagnostics and is
// normally the slash-separated path relative to the project root.
func NewFile(name string, src []byte) *File {
	lines := []int{0}
	for i, c := range src {
		if c == '\n' {
			lines = append(lines, i+1)
		}
	}
	return &File{name: name, src: src, lines: lines}
}

// Name returns the file name given to NewFile.
func (f *File) Name() string { return f.name }

// Source returns the source text. Callers must not modify it.
func (f *File) Source() []byte { return f.src }

// Line returns the 1-based line of p, or 0 for NoPos.
func (f *File) Line(p Pos) int {
	if !p.IsValid() {
		return 0
	}
	off := f.clamp(p.Offset())
	return sort.Search(len(f.lines), func(i int) bool { return f.lines[i] > off })
}

// Position returns the line and column of p. Positions past the end of the
// file report the column just after the last character.
func (f *File) Position(p Pos) Position {
	if !p.IsValid() {
		return Position{}
	}
	off := f.clamp(p.Offset())
	line := f.Line(p)
	start := f.lines[line-1]
	return Position{Line: line, Col: utf8.RuneCount(f.src[start:off]) + 1}
}

// LineText returns line n without its line ending, or "" if n is out of
// range.
func (f *File) LineText(n int) string {
	if n < 1 || n > len(f.lines) {
		return ""
	}
	start := f.lines[n-1]
	end := len(f.src)
	if n < len(f.lines) {
		end = f.lines[n] - 1
	}
	if end > start && f.src[end-1] == '\r' {
		end--
	}
	return string(f.src[start:end])
}

// LineEnd returns the position just after the last character of line n,
// before any line ending.
func (f *File) LineEnd(n int) Pos {
	if n < 1 || n > len(f.lines) {
		return NoPos
	}
	return PosAt(f.lines[n-1] + len(f.LineText(n)))
}

// Text returns the source between start and end.
func (f *File) Text(start, end Pos) string {
	if !start.IsValid() || !end.IsValid() {
		return ""
	}
	a, b := f.clamp(start.Offset()), f.clamp(end.Offset())
	if a > b {
		return ""
	}
	return string(f.src[a:b])
}

// EOF returns the position just after the last byte of the file.
func (f *File) EOF() Pos { return PosAt(len(f.src)) }

func (f *File) clamp(off int) int {
	switch {
	case off < 0:
		return 0
	case off > len(f.src):
		return len(f.src)
	}
	return off
}
