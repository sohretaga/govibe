package sqlgen

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/project"
)

func load(t *testing.T, path string) *ir.Program {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	prog, diags := project.Check([]project.Source{{Name: "main.vibe", Data: data}})
	for _, d := range diags {
		t.Errorf("%v", d)
	}
	if prog == nil {
		t.FailNow()
	}
	return prog
}

// describe prints a parameter expression for the golden file.
func describe(e ir.Expr) string {
	switch e := e.(type) {
	case *ir.VarRef:
		return e.Var.Name
	case *ir.Const:
		return e.String()
	case *ir.Binary:
		return "(" + describe(e.X) + " " + e.Op + " " + describe(e.Y) + ")"
	case *ir.Unary:
		return e.Op + " " + describe(e.X)
	}
	return fmt.Sprintf("%T", e)
}

var uses = [...]string{Value: "value", Limit: "limit", Offset: "offset"}

func TestGolden(t *testing.T) {
	for _, name := range []string{"queries", "auth"} {
		t.Run(name, func(t *testing.T) {
			golden.Assert(t, "testdata/"+name+".sql.golden", []byte(dump(t, "testdata/"+name+".vibe")))
		})
	}
}

// dump prints the schema and every statement of a program.
func dump(t *testing.T, path string) string {
	prog := load(t, path)
	var b strings.Builder
	if prog.Auth != nil {
		fmt.Fprintf(&b, "%s;\n%s;\n%s;\n%s;\n%s;\n\n", SessionUser(prog.Auth), SessionInsert, SessionRenew, SessionDelete, SessionPurge)
	}
	for _, tb := range Schema(prog) {
		if tb.Model == nil {
			fmt.Fprintf(&b, "-- table %s\n%s;\n%s\n\n", tb.Name, tb.Create, strings.Join(tb.Indexes, ";\n")+";")
			continue
		}
		fmt.Fprintf(&b, "-- table %s (model %s)\n%s;\n", tb.Name, tb.Model.Name, tb.Create)
		for _, ix := range tb.Indexes {
			fmt.Fprintf(&b, "%s;\n", ix)
		}
		for _, c := range tb.Columns {
			if c.Add == "" {
				fmt.Fprintf(&b, "-- %s cannot be added to a table with rows\n", c.Name)
			} else {
				fmt.Fprintf(&b, "%s;\n", c.Add)
			}
		}
		m := tb.Model
		fmt.Fprintf(&b, "%s;\n%s;\n%s;\n%s;\n%s;\n", List(m), Find(m, m.Field("id")), Insert(m), Update(m), Delete(m))
		for _, f := range m.Fields {
			switch {
			case f.Unique && f.Name != "id":
				fmt.Fprintf(&b, "%s;\n", Exists(m, f))
			case f.Ref != nil:
				fmt.Fprintf(&b, "%s;\n", Exists(f.Ref, f.Ref.Field("id")))
			}
		}
		b.WriteString("\n")
	}
	for _, r := range prog.Routes {
		if r.Origin == ir.FromCrud && r.Private && r.Op == "list" {
			fmt.Fprintf(&b, "-- %s %s\n%s;\n", r.Method, r.Path, ListOwned(r.Model, r.Owner))
		}
		if r.Origin != ir.FromRoute {
			continue
		}
		ir.Inspect(r.Value, func(e ir.Expr) bool {
			switch e := e.(type) {
			case *ir.QueryExpr:
				p := Select(e)
				fmt.Fprintf(&b, "-- %s %s\n%s;\n", r.Method, r.Path, p.SQL)
				for i, prm := range p.Params {
					fmt.Fprintf(&b, "--   ?%d = %s (%s, %s, bound as %s)\n", i+1, describe(prm.Expr), prm.Expr.Type(), uses[prm.Use], prm.Want)
				}
				return false
			case *ir.Find:
				fmt.Fprintf(&b, "-- %s %s\n%s;\n--   ?1 = %s\n", r.Method, r.Path, Find(e.Model, e.Field), describe(e.Value))
				return false
			}
			return true
		})
	}
	return b.String()
}

func TestLiteral(t *testing.T) {
	k := func(v any, kind ir.Kind) *ir.Const {
		c := &ir.Const{Value: v}
		c.T = ir.Basic(kind)
		return c
	}
	tests := []struct {
		c    *ir.Const
		want string
	}{
		{k(nil, ir.None), "NULL"},
		{k(true, ir.Bool), "1"},
		{k(false, ir.Bool), "0"},
		{k(int64(999), ir.Money), "999"},
		{k(2.0, ir.Float), "2.0"},
		{k(2.5, ir.Float), "2.5"},
		{k(1e300, ir.Float), "1e+300"},
		{k("it's", ir.Text), "'it''s'"},
	}
	for _, tt := range tests {
		if got := Literal(tt.c); got != tt.want {
			t.Errorf("Literal(%v) = %q, want %q", tt.c.Value, got, tt.want)
		}
	}
}

func TestIdent(t *testing.T) {
	if ident("order") != `"order"` || ident("book") != "book" || ident("group") != `"group"` {
		t.Error("ident quotes the wrong names")
	}
}
