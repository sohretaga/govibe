package gogen

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sohretaga/govibe/internal/project"
)

// FuzzGenerate checks that every program vibe check accepts generates
// Go that parses and is formatted, or reports a construct this version
// does not support; it never panics.
func FuzzGenerate(f *testing.F) {
	for _, pattern := range []string{"../../testdata/gen/*.vibe", "../../testdata/parse/*.vibe", "../../testdata/check/*.vibe", "../../internal/sqlgen/testdata/*.vibe"} {
		paths, _ := filepath.Glob(pattern)
		for _, path := range paths {
			if src, err := os.ReadFile(path); err == nil {
				f.Add(string(src))
			}
		}
	}
	f.Fuzz(func(t *testing.T, src string) {
		prog, _ := project.Check([]project.Source{{Name: "main.vibe", Data: []byte(src)}})
		if prog == nil {
			return
		}
		_, err := Generate(prog)
		var u *Unsupported
		if err != nil && !errors.As(err, &u) {
			t.Fatalf("Generate: %v", err)
		}
	})
}
