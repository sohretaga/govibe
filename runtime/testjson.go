package vibe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// JSON is a dynamic JSON value in a test, such as the body of a response.
// Its fields are checked when the test runs.
type JSON struct {
	v    any    // map[string]any, []any, string, json.Number, bool or nil
	path string // where the value comes from, such as body.user.name
}

// decodeJSON reads a response body. A body that is not JSON is its text.
func decodeJSON(b []byte, path string) JSON {
	if len(bytes.TrimSpace(b)) == 0 {
		return JSON{path: path}
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return JSON{v: string(b), path: path}
	}
	return JSON{v: v, path: path}
}

// Field returns a field of an object. A field the object does not have
// fails the test, naming the fields it has.
func (j JSON) Field(name string) (JSON, error) {
	m, ok := j.v.(map[string]any)
	if !ok {
		return JSON{}, fmt.Errorf("%s is %s, not an object, so it has no field %s", j.path, Show(j), name)
	}
	v, ok := m[name]
	if !ok {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		has := "no fields"
		if len(keys) > 0 {
			has = strings.Join(keys, ", ")
		}
		hint := ""
		if near := closest(name, keys); near != "" {
			hint = " (did you mean " + near + "?)"
		}
		return JSON{}, fmt.Errorf("%s has no field %s%s; it has %s", j.path, name, hint, has)
	}
	return JSON{v: v, path: j.path + "." + name}, nil
}

// MarshalJSON writes the value as it came.
func (j JSON) MarshalJSON() ([]byte, error) { return json.Marshal(j.v) }

// text returns the value as str does: a string as it is, none as "none",
// anything else as JSON.
func (j JSON) text() string {
	switch v := j.v.(type) {
	case string:
		return v
	case nil:
		return "none"
	}
	b, err := marshal(j.v)
	if err != nil {
		return fmt.Sprint(j.v)
	}
	return strings.TrimSpace(string(b))
}

// As converts a JSON value to the Go type of a GoVibe type, named typ for
// the message when it does not fit.
func As[T any](j JSON, typ string) (T, error) {
	var out T
	if j.v == nil {
		if k := reflect.TypeFor[T]().Kind(); k == reflect.Pointer || k == reflect.Slice {
			return out, nil
		}
		return out, fmt.Errorf("%s is none, not %s", j.path, typ)
	}
	b, err := json.Marshal(j.v)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	if err != nil {
		return out, fmt.Errorf("%s is %s, not %s", j.path, Show(j), typ)
	}
	return out, nil
}

// JSONLen returns len of a JSON value: the characters of a string or the
// elements of a list.
func JSONLen(j JSON) (int64, error) {
	switch v := j.v.(type) {
	case string:
		return Len(v), nil
	case []any:
		return int64(len(v)), nil
	}
	return 0, fmt.Errorf("len needs text or a list, and %s is %s", j.path, Show(j))
}

// Compare applies a comparison of a test to a and b, either of which may
// be JSON. Values compare as the API writes them in JSON, and a number
// equals a JSON string that holds the same number, so "9.99" == 9.99.
func Compare(op string, a, b any) (bool, error) {
	x, y := asPlain(a), asPlain(b)
	switch op {
	case "==":
		return equal(x, y), nil
	case "!=":
		return !equal(x, y), nil
	case "contains":
		xs, ok1 := x.(string)
		ys, ok2 := y.(string)
		if !ok1 || !ok2 {
			return false, fmt.Errorf("contains needs text on both sides, not %s and %s", Show(a), Show(b))
		}
		return Contains(xs, ys), nil
	}
	c, ok := order(x, y)
	if !ok {
		return false, fmt.Errorf("cannot compare %s and %s with %s", Show(a), Show(b), op)
	}
	switch op {
	case "<":
		return c < 0, nil
	case "<=":
		return c <= 0, nil
	case ">":
		return c > 0, nil
	case ">=":
		return c >= 0, nil
	}
	return false, fmt.Errorf("unknown comparison %s", op)
}

// asPlain returns a value as decoded JSON: what the API would write for it.
func asPlain(v any) any {
	switch v := v.(type) {
	case JSON:
		return v.v
	case Status:
		return v.String()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return decodeJSON(b, "").v
}

func equal(x, y any) bool {
	if p, q, ok := numbers(x, y); ok {
		return p.Cmp(q) == 0
	}
	switch x := x.(type) {
	case nil:
		return y == nil
	case bool:
		v, ok := y.(bool)
		return ok && x == v
	case string:
		v, ok := y.(string)
		return ok && x == v
	case []any:
		v, ok := y.([]any)
		return ok && slices.EqualFunc(x, v, equal)
	case map[string]any:
		v, ok := y.(map[string]any)
		if !ok || len(x) != len(v) {
			return false
		}
		for k, a := range x {
			b, ok := v[k]
			if !ok || !equal(a, b) {
				return false
			}
		}
		return true
	}
	return false
}

// order compares two numbers, two times or two strings.
func order(x, y any) (int, bool) {
	if p, q, ok := numbers(x, y); ok {
		return p.Cmp(q), true
	}
	xs, ok1 := x.(string)
	ys, ok2 := y.(string)
	if !ok1 || !ok2 {
		return 0, false
	}
	if a, err := time.Parse(time.RFC3339Nano, xs); err == nil {
		if b, err := time.Parse(time.RFC3339Nano, ys); err == nil {
			return a.Compare(b), true
		}
	}
	return strings.Compare(xs, ys), true
}

// decimal is a number as text, such as Money in JSON.
var decimal = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)

// numbers returns x and y as exact numbers when at least one is a JSON
// number and the other a number or a string that holds one.
func numbers(x, y any) (*big.Rat, *big.Rat, bool) {
	_, xn := x.(json.Number)
	_, yn := y.(json.Number)
	if !xn && !yn {
		return nil, nil, false
	}
	p, ok1 := number(x)
	q, ok2 := number(y)
	return p, q, ok1 && ok2
}

func number(v any) (*big.Rat, bool) {
	var s string
	switch v := v.(type) {
	case json.Number:
		s = string(v)
	case string:
		if !decimal.MatchString(v) {
			return nil, false
		}
		s = v
	default:
		return nil, false
	}
	return new(big.Rat).SetString(s)
}

// maxShow is the most characters Show writes of one value.
const maxShow = 300

// Show returns a value as a failed expect prints it: text quoted, Money
// with two decimal places, a status by name, none for a missing value
// and anything else as JSON.
func Show(v any) string {
	var s string
	switch v := v.(type) {
	case Status:
		return v.String()
	case Money:
		return v.String()
	case nil:
		return "none"
	case JSON:
		if v.v == nil {
			return "none"
		}
		b, err := marshal(v.v)
		if err != nil {
			return fmt.Sprint(v.v)
		}
		s = strings.TrimSpace(string(b))
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return "none"
			}
			if rv.Elem().Kind() != reflect.Struct || rv.Elem().Type() == reflect.TypeFor[time.Time]() {
				return Show(rv.Elem().Interface())
			}
		}
		b, err := marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		s = strings.TrimSpace(string(b))
	}
	if utf8.RuneCountInString(s) > maxShow {
		s = string([]rune(s)[:maxShow]) + "…"
	}
	return s
}

// closest returns the name in names nearest to name, at most two edits
// away, or "".
func closest(name string, names []string) string {
	best, bestDist := "", 3
	for _, n := range names {
		if d := distance(name, n); d < bestDist {
			best, bestDist = n, d
		}
	}
	return best
}

// distance returns the Levenshtein distance between a and b.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
