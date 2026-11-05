package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sohretaga/govibe/internal/diag"
)

type explainJSON struct {
	OK          bool   `json:"ok"`
	Code        string `json:"code,omitempty"`
	Title       string `json:"title,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Before      string `json:"before,omitempty"`
	After       string `json:"after,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Explain runs "vibe explain": it prints the explanation of a diagnostic
// code with an example that triggers it and the example fixed.
func Explain(code string, jsonOut bool, stdout, stderr io.Writer) int {
	info, ok := diag.Lookup(diag.Code(strings.ToUpper(code)))
	if !ok {
		msg := fmt.Sprintf("unknown diagnostic code %q; codes look like V201", code)
		if jsonOut {
			if err := diag.WriteJSON(stdout, explainJSON{Error: msg}); err != nil {
				fmt.Fprintln(stderr, "vibe:", err)
			}
		} else {
			fmt.Fprintln(stderr, "vibe:", msg)
		}
		return ExitFailure
	}
	if jsonOut {
		out := explainJSON{OK: true, Code: string(info.Code), Title: info.Title, Explanation: info.Explanation, Before: info.Before, After: info.After}
		if err := diag.WriteJSON(stdout, out); err != nil {
			fmt.Fprintln(stderr, "vibe:", err)
			return ExitFailure
		}
		return ExitOK
	}
	fmt.Fprintf(stdout, "%s: %s\n\n%s\n\nBefore:\n\n%s\n\nAfter:\n\n%s\n",
		info.Code, info.Title, info.Explanation, indent(info.Before), indent(info.After))
	return ExitOK
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
