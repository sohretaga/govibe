package golden

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/")

// Update reports whether the tests run with -update.
func Update() bool { return *update }

// Assert compares got with the golden file at path. With -update it writes
// got to path instead.
func Assert(t testing.TB, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test -update to create it", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s (run go test -update and review the diff):\n%s", path, Diff(string(want), string(got)))
	}
}

// Diff describes the first difference between want and got with a few
// lines of context.
func Diff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	i := 0
	for i < len(w) && i < len(g) && w[i] == g[i] {
		i++
	}
	var b strings.Builder
	from := max(i-3, 0)
	for j := from; j < i; j++ {
		fmt.Fprintf(&b, "  %4d   %s\n", j+1, w[j])
	}
	for j := i; j < min(i+3, len(w)); j++ {
		fmt.Fprintf(&b, "- %4d   %s\n", j+1, w[j])
	}
	for j := i; j < min(i+3, len(g)); j++ {
		fmt.Fprintf(&b, "+ %4d   %s\n", j+1, g[j])
	}
	return b.String()
}
