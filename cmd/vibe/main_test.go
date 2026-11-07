package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	_ "github.com/sohretaga/govibe/internal/golden"
)

func TestRun(t *testing.T) {
	tests := []struct {
		args   []string
		code   int
		output string
	}{
		{nil, 2, "Usage:"},
		{[]string{"help"}, 0, "vibe check"},
		{[]string{"deploy"}, 2, `unknown command "deploy"`},
		{[]string{"help"}, 0, "vibe build"},
		{[]string{"build", "x"}, 2, "unexpected argument"},
		{[]string{"build", "-o"}, 2, "flag needs an argument"},
		{[]string{"check", "--yaml"}, 2, "flag provided but not defined"},
		{[]string{"check", "main.vibe"}, 2, "unexpected argument"},
		{[]string{"fmt", "x"}, 2, "unexpected argument"},
		{[]string{"help"}, 0, "vibe test"},
		{[]string{"test", "-run"}, 2, "flag needs an argument"},
		{[]string{"test", "x"}, 2, "unexpected argument"},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		code := run(context.Background(), tt.args, &out, &out)
		if code != tt.code || !strings.Contains(out.String(), tt.output) {
			t.Errorf("run(%q) = %d, %q; want %d and output containing %q", tt.args, code, out.String(), tt.code, tt.output)
		}
	}
}
