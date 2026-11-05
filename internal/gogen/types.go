package gogen

import (
	"fmt"

	"github.com/sohretaga/govibe/internal/ir"
)

// goType returns the Go type of a GoVibe type. Records are pointers, so an
// optional record is the same pointer type; other optionals are pointers
// to their value.
func goType(t *ir.Type) string {
	switch t.Kind {
	case ir.Int:
		return "int64"
	case ir.Float:
		return "float64"
	case ir.Text, ir.Email, ir.Slug, ir.ID:
		return "string"
	case ir.Bool:
		return "bool"
	case ir.Time:
		return "time.Time"
	case ir.Money:
		return "vibe.Money"
	case ir.Record:
		return "*" + t.Model.Name
	case ir.List:
		return "[]" + goType(t.Elem)
	case ir.Query:
		return "[]*" + t.Model.Name
	case ir.Optional:
		if t.Elem.Kind == ir.Record {
			return goType(t.Elem)
		}
		return "*" + goType(t.Elem)
	}
	panic(fmt.Sprintf("gogen: no Go type for %s", t))
}

// inputKind returns the vibe.Kind that reads a param of type t, or "" for
// a type params cannot have.
func inputKind(t *ir.Type) string {
	if t.Kind == ir.Optional {
		t = t.Elem
	}
	switch t.Kind {
	case ir.Int:
		return "vibe.AsInt"
	case ir.Float:
		return "vibe.AsFloat"
	case ir.Text:
		return "vibe.AsText"
	case ir.Email:
		return "vibe.AsEmail"
	case ir.Slug:
		return "vibe.AsSlug"
	case ir.ID:
		return "vibe.AsID"
	case ir.Bool:
		return "vibe.AsBool"
	case ir.Money:
		return "vibe.AsMoney"
	case ir.Time:
		return "vibe.AsTime"
	}
	return ""
}

// elem returns the value type of an optional, or t itself.
func elem(t *ir.Type) *ir.Type {
	if t.Kind == ir.Optional {
		return t.Elem
	}
	return t
}
