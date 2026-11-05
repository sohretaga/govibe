package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/sohretaga/govibe/internal/diag"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/project"
)

// routeJSON is one route in the output of "vibe routes --json".
type routeJSON struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Params  []paramJSON `json:"params"`
	Result  string      `json:"result"`
	Status  string      `json:"status"`
	Login   bool        `json:"login"`
	Origin  string      `json:"origin"`
	Feature string      `json:"feature,omitempty"`
	File    string      `json:"file,omitempty"`
	Line    int         `json:"line,omitempty"`
}

type paramJSON struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	In      string `json:"in"`
	Default string `json:"default,omitempty"`
}

var (
	origins = map[ir.Origin]string{ir.FromRoute: "route", ir.FromCrud: "crud", ir.FromAuth: "auth", ir.FromBuiltin: "builtin"}
	sources = map[ir.Source]string{ir.InPath: "path", ir.InQuery: "query", ir.InBody: "body"}
)

// Routes runs "vibe routes" on the project in dir: it lists every route,
// including those that crud and auth generate.
func Routes(ctx context.Context, dir string, jsonOut bool, stdout, stderr io.Writer) int {
	srcs, err := project.Read(ctx, dir)
	if err != nil {
		return fail(err, jsonOut, stdout, stderr)
	}
	prog, diags := project.Check(srcs)
	r := report(diags)
	if prog != nil {
		for _, rt := range prog.Routes {
			r.Routes = append(r.Routes, toJSON(rt))
		}
	}
	if jsonOut {
		return emit(r, stdout, stderr)
	}
	if err := diag.WriteText(stderr, r.Diagnostics); err != nil {
		return ExitFailure
	}
	if prog != nil {
		writeRoutes(stdout, r.Routes)
	}
	return code(r)
}

func toJSON(r *ir.Route) routeJSON {
	out := routeJSON{
		Method: r.Method, Path: r.Path, Result: r.Result.String(), Status: r.Status, Login: r.Login,
		Origin: origins[r.Origin], Feature: r.Feature, File: r.Pos.File, Line: r.Pos.Line,
		Params: []paramJSON{},
	}
	if r.Result.Kind == ir.Empty {
		out.Result = "empty"
	}
	switch r.Origin {
	case ir.FromCrud:
		out.Origin = "crud " + r.Model.Name + " " + r.Op
	case ir.FromAuth:
		out.Origin = "auth " + r.Op
	}
	for _, p := range r.Params {
		pj := paramJSON{Name: p.Name, Type: p.Type.String(), In: sources[p.In]}
		if p.Default != nil {
			pj.Default = p.Default.String()
		}
		out.Params = append(out.Params, pj)
	}
	return out
}

func writeRoutes(w io.Writer, routes []routeJSON) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "METHOD\tPATH\tPARAMS\tRESULT\tLOGIN\tFROM")
	for _, r := range routes {
		var params []string
		for _, p := range r.Params {
			if p.In == "path" {
				continue
			}
			s := p.Name + ": " + p.Type
			if p.Default != "" {
				s += " = " + p.Default
			}
			params = append(params, s)
		}
		login := ""
		if r.Login {
			login = "yes"
		}
		from := r.Origin
		if r.File != "" {
			from += fmt.Sprintf(" (%s:%d)", r.File, r.Line)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Method, r.Path, strings.Join(params, ", "), r.Result, login, from)
	}
	tw.Flush()
}
