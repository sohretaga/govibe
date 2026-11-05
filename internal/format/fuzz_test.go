package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sohretaga/govibe/internal/parser"
	"github.com/sohretaga/govibe/internal/token"
)

// FuzzFormat checks the round-trip properties on every input that parses.
func FuzzFormat(f *testing.F) {
	paths, _ := filepath.Glob("../../testdata/parse/*.vibe")
	for _, path := range paths {
		if src, err := os.ReadFile(path); err == nil {
			f.Add(string(src))
		}
	}
	f.Fuzz(func(t *testing.T, src string) {
		if _, diags := parser.ParseFile(token.NewFile("fuzz.vibe", []byte(src))); len(diags) > 0 {
			return
		}
		roundTrip(t, "fuzz.vibe", []byte(src))
	})
}
