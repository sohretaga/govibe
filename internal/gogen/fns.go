package gogen

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/sohretaga/govibe/internal/ir"
)

// fns returns fns.go: the fns of the program, top-level and per feature.
func (g *gen) fns() string {
	var b strings.Builder
	b.WriteString(header)
	all := append([]*ir.Func{}, g.prog.Funcs...)
	for _, ft := range g.prog.Features {
		all = append(all, ft.Funcs...)
	}
	for _, fn := range all {
		if fn.Extern {
			g.unsupported(fn.Pos, "extern fn")
		}
		f := newBody(g)
		f.fn = fn
		f.markUsed(fn.Body)
		params := make([]string, len(fn.Params))
		for i, p := range fn.Params {
			params[i] = f.local(p.Var) + " " + goType(p.Type)
		}
		f.block(fn.Body)
		owner := "fn " + fn.Name
		if fn.Feature != "" {
			owner += " of feature " + fn.Feature
		}
		fmt.Fprintf(&b, "\n// %s is %s, declared at %s.\nfunc %s(%s) %s {\n%s}\n",
			fnName(fn), owner, where(fn.Pos), fnName(fn), strings.Join(params, ", "), goType(fn.Result), f.w.String())
	}
	return b.String()
}

var mainTemplate = template.Must(template.New("main.go").Parse(`// main starts {{.Name}}. Flags override environment variables, which
// override the app block: --port or VIBE_PORT, --db or VIBE_DB.
func main() {
	sqlite.MustRegisterDeterministicScalarFunction("vibe_contains", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		return vibe.SQLContains(args)
	})
	app := &vibe.App{
		Name:    {{printf "%q" .Name}},
		DB:      {{printf "%q" .DB}},
		Tables:  tables(),
		Queries: queries(),
		Routes:  routes(),
	}
	os.Exit(app.Main(os.Args[1:]))
}
`))

// main returns main.go.
func (g *gen) main() string {
	app := g.prog.App
	db := app.DB
	if db == "" {
		db = "sqlite://" + app.Snake + ".db"
	}
	var b strings.Builder
	b.WriteString(header)
	if err := mainTemplate.Execute(&b, struct{ Name, DB string }{app.Name, db}); err != nil {
		panic(err)
	}
	return b.String()
}
