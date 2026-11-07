package ir

import (
	"testing"

	_ "github.com/sohretaga/govibe/internal/golden"
)

func TestTypeString(t *testing.T) {
	book := &Model{Name: "Book"}
	tests := []struct {
		t    *Type
		want string
	}{
		{Basic(Int), "Int"},
		{OptionalOf(Basic(Text)), "Text?"},
		{OptionalOf(OptionalOf(Basic(Text))), "Text?"},
		{ListOf(ModelOf(book)), "[Book]"},
		{OptionalOf(ListOf(Basic(ID))), "[ID]?"},
		{QueryOf(book), "Query Book"},
		{Basic(Empty), "no value"},
		{Basic(JSON), "Json"},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
	if !Identical(ListOf(ModelOf(book)), QueryOf(book).Value()) {
		t.Error("a query used as a value is a list of its model")
	}
	if Identical(ModelOf(book), ModelOf(&Model{Name: "Book"})) {
		t.Error("records of different models are not identical")
	}
}

func TestConstString(t *testing.T) {
	c := func(k Kind, v any) *Const {
		x := &Const{Value: v}
		x.T = Basic(k)
		return x
	}
	tests := []struct {
		c    *Const
		want string
	}{
		{c(Int, int64(-12)), "-12"},
		{c(Money, int64(750)), "7.50"},
		{c(Money, int64(-5)), "-0.05"},
		{c(Float, 2.5), "2.5"},
		{c(Text, "a\"b"), `"a\"b"`},
		{c(Bool, true), "true"},
		{c(None, nil), "none"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestInspect(t *testing.T) {
	v := &Var{Name: "x", Type: Basic(Int)}
	ref := &VarRef{Var: v}
	one := &Const{Value: int64(1)}
	sum := &Binary{Op: "+", X: ref, Y: one}
	q := &QueryExpr{Where: []Expr{sum}, Limit: ref}
	body := &Block{Stmts: []Stmt{
		&Let{Var: v, Value: q},
		&If{Cond: ref, Then: &Block{Stmts: []Stmt{&Return{Value: one}}}, Else: &Block{Stmts: []Stmt{&Fail{Message: one}}}},
		&ExprStmt{X: &Call{Args: []Expr{ref}}},
	}}
	count := map[string]int{}
	InspectBlock(body, func(e Expr) bool {
		switch e.(type) {
		case *VarRef:
			count["ref"]++
		case *Const:
			count["const"]++
		case *QueryExpr:
			count["query"]++
		}
		return true
	})
	if count["ref"] != 4 || count["const"] != 3 || count["query"] != 1 {
		t.Errorf("visited %v, want 4 refs, 3 consts and 1 query", count)
	}
	n := 0
	Inspect(q, func(Expr) bool { n++; return false })
	if n != 1 {
		t.Errorf("Inspect visited %d expressions after f returned false, want 1", n)
	}
}
