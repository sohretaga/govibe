package vibe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// maxBody is the largest request body the runtime reads.
const maxBody = 1 << 20

// Errors collects input problems per field. A value of the wrong type or
// format is a bad_request problem; a value that breaks a field rule is an
// invalid one. The first message for a field wins.
type Errors struct {
	bad     map[string]string
	invalid map[string]string
}

// Bad records a value of the wrong type or format.
func (e *Errors) Bad(field, msg string) {
	if e.bad == nil {
		e.bad = map[string]string{}
	}
	if _, ok := e.bad[field]; !ok {
		e.bad[field] = msg
	}
}

// Invalid records a value that breaks a field rule.
func (e *Errors) Invalid(field, msg string) {
	if e.invalid == nil {
		e.invalid = map[string]string{}
	}
	if _, ok := e.invalid[field]; !ok {
		e.invalid[field] = msg
	}
}

// Err returns nil without problems; otherwise a bad_request error when
// some value has the wrong type, else an invalid error.
func (e *Errors) Err() error {
	switch {
	case len(e.bad) > 0:
		return &Error{Status: BadRequest, Message: statuses[BadRequest].message, Fields: e.bad}
	case len(e.invalid) > 0:
		return &Error{Status: Invalid, Message: statuses[Invalid].message, Fields: e.invalid}
	}
	return nil
}

// Kind tells Input how to read a value.
type Kind int

// Input kinds, one per GoVibe value type.
const (
	AsText Kind = iota
	AsEmail
	AsSlug
	AsID
	AsInt
	AsFloat
	AsBool
	AsMoney
	AsTime
)

// Input holds the params of a request, from the query string or from a
// JSON object body, and the problems found while reading them.
type Input struct {
	Errors
	body     map[string]json.RawMessage
	query    url.Values
	used     map[string]bool
	readOnly map[string]bool
}

// queryInput reads params from the query string of r.
func queryInput(r *http.Request) *Input {
	return &Input{query: r.URL.Query(), used: map[string]bool{}}
}

// bodyInput reads params from the JSON object in the body of r. An empty
// body is an empty object.
func bodyInput(w http.ResponseWriter, r *http.Request) (*Input, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, Fail(BadRequest, "the request body is larger than 1 MB")
		}
		return nil, Fail(BadRequest, "cannot read the request body")
	}
	in := &Input{body: map[string]json.RawMessage{}, used: map[string]bool{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return in, nil
	}
	if err := json.Unmarshal(data, &in.body); err != nil || in.body == nil {
		return nil, Fail(BadRequest, "the body must be a JSON object")
	}
	return in, nil
}

// ReadOnly names fields the client may see but not set, so giving them
// answers "is not writable" instead of "unknown field".
func (in *Input) ReadOnly(names ...string) {
	if in.readOnly == nil {
		in.readOnly = map[string]bool{}
	}
	for _, n := range names {
		in.readOnly[n] = true
	}
}

// Has reports whether the request gives name, even as null.
func (in *Input) Has(name string) bool {
	in.used[name] = true
	if in.body != nil {
		_, ok := in.body[name]
		return ok
	}
	_, ok := in.query[name]
	return ok
}

// Err records the body fields that no param read, as Done does, and
// reports every problem found.
func (in *Input) Err() error {
	in.Done()
	return in.Errors.Err()
}

// Done records every body field that no param read: "is not writable"
// for the names given to ReadOnly, "unknown field" for the others.
func (in *Input) Done() {
	for name := range in.body {
		switch {
		case in.used[name]:
		case in.readOnly[name]:
			in.Invalid(name, "is not writable")
		default:
			in.Invalid(name, "unknown field")
		}
	}
}

type state int

const (
	absent state = iota
	null
	bad
	present
)

// value reads name as kind k.
func (in *Input) value(name string, k Kind) (any, state) {
	in.used[name] = true
	if in.body != nil {
		raw, ok := in.body[name]
		switch {
		case !ok:
			return nil, absent
		case string(raw) == "null":
			return nil, null
		}
		v, msg := fromJSON(raw, k)
		return in.check(name, v, msg, k)
	}
	vs, ok := in.query[name]
	if !ok || len(vs) == 0 {
		return nil, absent
	}
	v, msg := fromText(vs[0], k)
	return in.check(name, v, msg, k)
}

func (in *Input) check(name string, v any, msg string, k Kind) (any, state) {
	if msg != "" {
		in.Bad(name, msg)
		return nil, bad
	}
	switch s, _ := v.(string); {
	case k == AsEmail && !ValidEmail(s):
		in.Invalid(name, "must be a valid email")
	case k == AsSlug && !ValidSlug(s):
		in.Invalid(name, "must be lowercase letters, digits and dashes, like salam-dunya")
	}
	return v, present
}

var kindMessages = [...]string{
	AsText:  "must be text",
	AsEmail: "must be text",
	AsSlug:  "must be text",
	AsID:    "must be text",
	AsInt:   "must be a whole number",
	AsFloat: "must be a number",
	AsBool:  "must be true or false",
	AsMoney: "must be an amount like 9.99",
	AsTime:  "must be a time like 2026-01-02T15:04:05Z",
}

// fromJSON decodes one JSON value. It returns a message when the value
// has the wrong type.
func fromJSON(raw json.RawMessage, k Kind) (any, string) {
	switch k {
	case AsText, AsEmail, AsSlug, AsID, AsTime:
		var s string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
			return nil, kindMessages[k]
		}
		return fromText(s, k)
	case AsBool:
		switch string(raw) {
		case "true":
			return true, ""
		case "false":
			return false, ""
		}
		return nil, kindMessages[k]
	case AsMoney:
		var m Money
		if err := m.UnmarshalJSON(raw); err != nil {
			return nil, err.Error()
		}
		return m, ""
	}
	if len(raw) == 0 || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return nil, kindMessages[k]
	}
	return fromText(string(raw), k)
}

// fromText decodes a value from its text, as a query string gives it.
func fromText(s string, k Kind) (any, string) {
	switch k {
	case AsInt:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, kindMessages[k]
		}
		return n, ""
	case AsFloat:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, kindMessages[k]
		}
		return f, ""
	case AsBool:
		switch s {
		case "true":
			return true, ""
		case "false":
			return false, ""
		}
		return nil, kindMessages[k]
	case AsMoney:
		m, err := ParseMoney(s)
		if err != nil {
			return nil, err.Error()
		}
		return m, ""
	case AsTime:
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, kindMessages[k]
		}
		return t.UTC(), ""
	}
	return s, ""
}

// Req reads a required param; a missing one is "is required".
func Req[T any](in *Input, name string, k Kind) T {
	v, s := in.value(name, k)
	if s != present {
		if s != bad {
			in.Invalid(name, "is required")
		}
		var zero T
		return zero
	}
	return v.(T)
}

// Opt reads an optional param: nil when it is missing or null.
func Opt[T any](in *Input, name string, k Kind) *T {
	v, s := in.value(name, k)
	if s != present {
		return nil
	}
	t := v.(T)
	return &t
}

// Def reads a param with a default, used when it is missing or null.
func Def[T any](in *Input, name string, k Kind, def T) T {
	v, s := in.value(name, k)
	if s != present {
		return def
	}
	return v.(T)
}
