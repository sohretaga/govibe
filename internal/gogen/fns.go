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
			continue
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

// externs returns externs.go, which checks at compile time that every
// extern fn has a Go function of the declared signature in go/. Line
// directives put a mismatch at the extern fn line, as V903.
func (g *gen) externs() string {
	var b strings.Builder
	b.WriteString(header)
	for _, fn := range Externs(g.prog) {
		params := []string{"context.Context"}
		for _, p := range fn.Params {
			params = append(params, goType(p.Type))
		}
		fmt.Fprintf(&b, "\n// %s must be func(%s) (%s, error).\nvar _ = []func(%s) (%s, error){\n%s%s\n%s,\n%s\n}\n",
			externName(fn), strings.Join(params, ", "), goType(fn.Result), strings.Join(params, ", "), goType(fn.Result),
			markAt, where(fn.Pos), externName(fn), markEnd)
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

// Externs returns every extern fn of p, top-level ones first.
func Externs(p *ir.Program) []*ir.Func {
	var out []*ir.Func
	for _, fn := range p.Funcs {
		if fn.Extern {
			out = append(out, fn)
		}
	}
	for _, ft := range p.Features {
		for _, fn := range ft.Funcs {
			if fn.Extern {
				out = append(out, fn)
			}
		}
	}
	return out
}

// ExternFunc returns the Go name of an extern fn in package ext and the
// signature its Go function must have.
func ExternFunc(fn *ir.Func) (name, signature string) {
	params := []string{"ctx context.Context"}
	for _, p := range fn.Params {
		params = append(params, p.Name+" "+goType(p.Type))
	}
	name = strings.TrimPrefix(externName(fn), "ext.")
	return name, "func " + name + "(" + strings.Join(params, ", ") + ") (" + goType(fn.Result) + ", error)"
}
