package token

import (
	"testing"

	_ "github.com/sohretaga/govibe/internal/golden"
)

func TestLookup(t *testing.T) {
	words := Keywords()
	if len(words) != 35 {
		t.Fatalf("got %d keywords, the spec defines 35", len(words))
	}
	for _, w := range words {
		k := Lookup(w)
		if !k.IsKeyword() || k.String() != w {
			t.Errorf("Lookup(%q) = %v", w, k)
		}
	}
	for _, w := range []string{"title", "Book", "Text", "ok", "created", "list"} {
		if k := Lookup(w); k != Illegal {
			t.Errorf("Lookup(%q) = %v, want Illegal", w, k)
		}
	}
}
