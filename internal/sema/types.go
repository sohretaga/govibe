package sema

import "github.com/sohretaga/govibe/internal/ir"

// assignable reports whether a value of type from can fill a place of type
// to, under the conversion rules of the spec.
func assignable(from, to *ir.Type) bool {
	switch {
	case from.Kind == ir.Invalid || to.Kind == ir.Invalid:
		return true
	case from.Kind == ir.JSON:
		// A test's JSON values are checked when the test runs.
		return to.Kind != ir.Empty && to.Kind != ir.ModelName
	case ir.Identical(from, to):
		return true
	}
	switch to.Kind {
	case ir.Optional:
		switch from.Kind {
		case ir.None:
			return true
		case ir.Optional:
			return assignable(from.Elem, to.Elem)
		}
		return assignable(from, to.Elem)
	case ir.Float:
		return from.Kind == ir.Int
	case ir.Text, ir.Email, ir.Slug:
		return from.IsText()
	case ir.List:
		switch from.Kind {
		case ir.Query:
			return to.Elem.Kind == ir.Record && to.Elem.Model == from.Model
		case ir.List:
			return assignable(from.Elem, to.Elem)
		}
	}
	return false
}

// comparable reports whether == and != apply to a and b.
func comparable(a, b *ir.Type) bool {
	if a.Kind == ir.JSON || b.Kind == ir.JSON || a.Kind == ir.Invalid || b.Kind == ir.Invalid {
		return true
	}
	for _, t := range []*ir.Type{a, b} {
		u := t
		if u.Kind == ir.Optional {
			u = u.Elem
		}
		switch u.Kind {
		case ir.Record, ir.List, ir.Query, ir.ModelName, ir.Empty, ir.Response:
			return false
		}
	}
	return assignable(a, b) || assignable(b, a)
}

// ordered reports whether < <= > >= apply to a and b.
func ordered(a, b *ir.Type) bool {
	switch {
	case a.Kind == ir.JSON || b.Kind == ir.JSON || a.Kind == ir.Invalid || b.Kind == ir.Invalid:
		return true
	case intOrFloat(a) && intOrFloat(b):
		return true
	case a.Kind == ir.Money && b.Kind == ir.Money, a.Kind == ir.Time && b.Kind == ir.Time:
		return true
	}
	return a.IsText() && b.IsText()
}

func intOrFloat(t *ir.Type) bool { return t.Kind == ir.Int || t.Kind == ir.Float }

// arithmetic returns the result of x op y, or nil if op does not apply.
func arithmetic(op string, x, y *ir.Type) *ir.Type {
	switch {
	case x.Kind == ir.Invalid || y.Kind == ir.Invalid:
		return ir.Basic(ir.Invalid)
	case x.Kind == ir.Int && y.Kind == ir.Int:
		return ir.Basic(ir.Int)
	case op != "%" && intOrFloat(x) && intOrFloat(y):
		return ir.Basic(ir.Float)
	case (op == "+" || op == "-") && x.Kind == ir.Money && y.Kind == ir.Money:
		return ir.Basic(ir.Money)
	case op == "*" && (x.Kind == ir.Money && y.Kind == ir.Int || x.Kind == ir.Int && y.Kind == ir.Money):
		return ir.Basic(ir.Money)
	}
	return nil
}
