package token

import (
	"testing"

	_ "github.com/sohretaga/govibe/internal/golden"
)

func TestPosition(t *testing.T) {
	f := NewFile("main.vibe", []byte("ab\n  \"Dünya\" x\r\n\nlast"))
	tests := []struct {
		offset int
		want   Position
	}{
		{0, Position{1, 1}},
		{2, Position{1, 3}},
		{3, Position{2, 1}},
		{5, Position{2, 3}},
		{12, Position{2, 9}},  // the closing quote: ü is two bytes but one column
		{16, Position{2, 13}}, // the \n of a CRLF line ending
		{17, Position{3, 1}},
		{18, Position{4, 1}},
		{22, Position{4, 5}}, // end of file
		{99, Position{4, 5}}, // clamped
	}
	for _, tt := range tests {
		if got := f.Position(PosAt(tt.offset)); got != tt.want {
			t.Errorf("Position(PosAt(%d)) = %v, want %v", tt.offset, got, tt.want)
		}
	}
	if got := f.Position(NoPos); got != (Position{}) {
		t.Errorf("Position(NoPos) = %v, want zero", got)
	}
}

func TestLineText(t *testing.T) {
	f := NewFile("main.vibe", []byte("one\r\ntwo\n\nfour"))
	for n, want := range map[int]string{0: "", 1: "one", 2: "two", 3: "", 4: "four", 5: ""} {
		if got := f.LineText(n); got != want {
			t.Errorf("LineText(%d) = %q, want %q", n, got, want)
		}
	}
	if got, want := f.LineEnd(1), PosAt(3); got != want {
		t.Errorf("LineEnd(1) = %d, want %d", got, want)
	}
}
