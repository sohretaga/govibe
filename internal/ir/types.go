package ir

// Kind is the kind of a type.
type Kind int

// Type kinds.
const (
	Invalid   Kind = iota // a type error was reported; accepted everywhere
	Int                   // int64
	Float                 // float64
	Text                  // string
	Bool                  // bool
	Time                  // time.Time in UTC
	Money                 // int64 minor units
	Email                 // validated string
	ID                    // UUIDv7 string
	Slug                  // URL-friendly unique string
	None                  // the literal none, before it meets an optional
	Record                // a record of a model
	List                  // [Elem]
	Optional              // Elem?
	Query                 // a query over Model; a value of type [Model]
	Empty                 // no value
	Response              // the response of a request in a test
	Status                // a status name such as created or conflict
	JSON                  // a dynamic JSON value in a test
	ModelName             // a model name used as a value, as in Book.where
)

// Type is a GoVibe type.
type Type struct {
	Kind  Kind
	Elem  *Type  // element of List and Optional
	Model *Model // model of Record, Query and ModelName
}

// Basic returns the type of kind k, which must not need an element or a
// model.
func Basic(k Kind) *Type { return &Type{Kind: k} }

// ListOf returns [elem].
func ListOf(elem *Type) *Type { return &Type{Kind: List, Elem: elem} }

// OptionalOf returns elem?. An optional of an optional is the optional.
func OptionalOf(elem *Type) *Type {
	if elem.Kind == Optional || elem.Kind == Invalid {
		return elem
	}
	return &Type{Kind: Optional, Elem: elem}
}

// ModelOf returns the type of a value of model m.
func ModelOf(m *Model) *Type { return &Type{Kind: Record, Model: m} }

// QueryOf returns the type of a query over m.
func QueryOf(m *Model) *Type { return &Type{Kind: Query, Model: m} }

var kindNames = [...]string{
	Invalid:  "invalid",
	Int:      "Int",
	Float:    "Float",
	Text:     "Text",
	Bool:     "Bool",
	Time:     "Time",
	Money:    "Money",
	Email:    "Email",
	ID:       "ID",
	Slug:     "Slug",
	None:     "none",
	Empty:    "no value",
	Response: "Response",
	Status:   "Status",
	JSON:     "Json",
}

// String returns the type as GoVibe source writes it.
func (t *Type) String() string {
	switch t.Kind {
	case Record:
		return t.Model.Name
	case List:
		return "[" + t.Elem.String() + "]"
	case Optional:
		return t.Elem.String() + "?"
	case Query:
		return "Query " + t.Model.Name
	case ModelName:
		return "model " + t.Model.Name
	}
	if int(t.Kind) < len(kindNames) {
		return kindNames[t.Kind]
	}
	return "invalid"
}

// Identical reports whether a and b are the same type.
func Identical(a, b *Type) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case List, Optional:
		return Identical(a.Elem, b.Elem)
	case Record, Query, ModelName:
		return a.Model == b.Model
	}
	return true
}

// IsNumeric reports whether t is Int, Float or Money.
func (t *Type) IsNumeric() bool { return t.Kind == Int || t.Kind == Float || t.Kind == Money }

// IsText reports whether t is Text, Email or Slug, the types that hold
// text.
func (t *Type) IsText() bool { return t.Kind == Text || t.Kind == Email || t.Kind == Slug }

// IsScalar reports whether t is one of the built-in value types.
func (t *Type) IsScalar() bool { return t.Kind >= Int && t.Kind <= Slug }

// Value returns the type of t used as a value: a Query becomes a list of
// its model.
func (t *Type) Value() *Type {
	if t.Kind == Query {
		return ListOf(ModelOf(t.Model))
	}
	return t
}
