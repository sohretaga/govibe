package vibe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// newTestTester returns a Tester serving routes without a database.
func newTestTester(t *testing.T, routes []Route) *Tester {
	t.Helper()
	tt := &Tester{ctx: context.Background()}
	s := &server{log: slog.New(slog.NewJSONHandler(&tt.log, nil)), ids: newIDs(), limit: newLimiter(loginLimit)}
	mux, err := s.routes(routes)
	if err != nil {
		t.Fatal(err)
	}
	tt.mux = mux
	return tt
}

// echo answers the method, query, body and Authorization header of a
// request.
func echo(c *Ctx) error {
	body, err := io.ReadAll(c.r.Body)
	if err != nil {
		return err
	}
	return c.OK(map[string]any{
		"method": c.r.Method,
		"query":  c.r.URL.RawQuery,
		"body":   string(body),
		"auth":   c.r.Header.Get("Authorization"),
		"path":   c.r.URL.EscapedPath(),
	})
}

func TestTesterDo(t *testing.T) {
	tt := newTestTester(t, []Route{
		{"GET", "/echo/{x}", echo},
		{"POST", "/echo/{x}", echo},
		{"DELETE", "/gone", func(c *Ctx) error { return c.Empty() }},
		{"GET", "/div", divide},
	})
	price, when := Money(999), time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	field := func(res *Response, name string) string {
		t.Helper()
		v, err := res.Body.Field(name)
		if err != nil {
			t.Fatal(err)
		}
		return v.text()
	}

	res, err := tt.Do("GET", "/echo/"+PathSeg("a b/c"), With("q", "x y"), With("tag", []string{"a", "b"}), With("none", (*string)(nil)), With("price", &price))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != OK || field(res, "query") != "price=9.99&q=x+y&tag=a&tag=b" || field(res, "body") != "" || field(res, "path") != "/echo/a%20b%2Fc" {
		t.Errorf("GET = %v %s", res.Status, Show(res.Body))
	}

	tt.LoginAs("tok")
	res, err = tt.Do("POST", "/echo/x", With("title", "Dune"), With("price", price), With("at", when), With("gone", (*int64)(nil)), With("tags", []string{"a"}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"title":"Dune","price":"9.99","at":"2026-01-02T03:04:05Z","gone":null,"tags":["a"]}`
	if got := field(res, "body"); got != want {
		t.Errorf("POST body = %s, want %s", got, want)
	}
	if got := field(res, "auth"); got != "Bearer tok" {
		t.Errorf("Authorization = %q", got)
	}

	res, err = tt.Do("POST", "/echo/x")
	if err != nil {
		t.Fatal(err)
	}
	if got := field(res, "body"); got != "{}" {
		t.Errorf("POST without params sends %s, want {}", got)
	}

	res, err = tt.Do("DELETE", "/gone")
	if err != nil || res.Status != Empty || res.Body.v != nil {
		t.Errorf("DELETE = %v %v, %v", res.Status, res.Body.v, err)
	}
	res, err = tt.Do("GET", "/nowhere")
	if err != nil || res.Status != NotFound {
		t.Errorf("GET /nowhere = %v, %v", res, err)
	}
	code, err := res.Body.Field("error")
	if err == nil {
		code, err = code.Field("code")
	}
	if err != nil || code.text() != "not_found" {
		t.Errorf("error code = %v, %v", code, err)
	}

	res, err = tt.Do("GET", "/div")
	if err != nil || res.Status != Internal {
		t.Fatalf("GET /div = %v, %v", res, err)
	}
	if got := tt.appErrors(); !strings.HasPrefix(got, "the app answered internal at ") || !strings.HasSuffix(got, "books.vibe:12: panic: runtime error: integer divide by zero") {
		t.Errorf("appErrors = %q", got)
	}
}

func TestTesterPanic(t *testing.T) {
	tt := newTestTester(t, nil)
	err := tt.run(func(*Tester, *Ctx) error {
		zero := int64(len(tt.token))
//line books.vibe:30
		_ = 1 / zero
		return nil
	})
	f := asFailure(err)
	if !strings.HasSuffix(f.File, "books.vibe") || f.Line != 30 || !strings.Contains(f.Message, "divide by zero") {
		t.Errorf("panic failure = %+v", f)
	}
}

func jsonOf(t *testing.T, s string) JSON {
	t.Helper()
	return decodeJSON([]byte(s), "body")
}

func TestJSONField(t *testing.T) {
	body := jsonOf(t, `{"title":"Dune","user":{"name":"Ann"},"tags":[],"n":null}`)
	user, err := body.Field("user")
	if err != nil {
		t.Fatal(err)
	}
	name, err := user.Field("name")
	if err != nil || name.text() != "Ann" || name.path != "body.user.name" {
		t.Errorf("body.user.name = %v, %v", name, err)
	}
	for _, tt := range []struct {
		j    JSON
		name string
		want string
	}{
		{body, "titel", "body has no field titel (did you mean title?); it has n, tags, title, user"},
		{body, "price", "body has no field price; it has n, tags, title, user"},
		{name, "x", `body.user.name is "Ann", not an object, so it has no field x`},
		{jsonOf(t, `{}`), "x", "body has no field x; it has no fields"},
		{jsonOf(t, ``), "x", "body is none, not an object, so it has no field x"},
	} {
		if _, err := tt.j.Field(tt.name); err == nil || err.Error() != tt.want {
			t.Errorf("Field(%s) = %v, want %s", tt.name, err, tt.want)
		}
	}
	if text := jsonOf(t, `not json`); text.v != "not json" {
		t.Errorf("a body that is not JSON = %v", text.v)
	}
}

func TestAs(t *testing.T) {
	body := jsonOf(t, `{"n":412,"f":4.5,"s":"x","price":"9.99","b":true,"none":null,"at":"2026-01-02T03:04:05Z","list":["a"]}`)
	get := func(name string) JSON {
		v, err := body.Field(name)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if n, err := As[int64](get("n"), "Int"); n != 412 || err != nil {
		t.Errorf("Int = %v, %v", n, err)
	}
	if m, err := As[Money](get("price"), "Money"); m != 999 || err != nil {
		t.Errorf("Money = %v, %v", m, err)
	}
	if b, err := As[bool](get("b"), "Bool"); !b || err != nil {
		t.Errorf("Bool = %v, %v", b, err)
	}
	if at, err := As[time.Time](get("at"), "Time"); at.Year() != 2026 || err != nil {
		t.Errorf("Time = %v, %v", at, err)
	}
	if p, err := As[*string](get("none"), "Text?"); p != nil || err != nil {
		t.Errorf("Text? = %v, %v", p, err)
	}
	if l, err := As[[]string](get("list"), "[Text]"); len(l) != 1 || err != nil {
		t.Errorf("[Text] = %v, %v", l, err)
	}
	for _, tt := range []struct {
		name, typ, want string
		as              func(JSON, string) error
	}{
		{"s", "Int", `body.s is "x", not Int`, func(j JSON, typ string) error { _, err := As[int64](j, typ); return err }},
		{"f", "Int", `body.f is 4.5, not Int`, func(j JSON, typ string) error { _, err := As[int64](j, typ); return err }},
		{"n", "Text", `body.n is 412, not Text`, func(j JSON, typ string) error { _, err := As[string](j, typ); return err }},
		{"none", "Text", `body.none is none, not Text`, func(j JSON, typ string) error { _, err := As[string](j, typ); return err }},
		{"s", "Bool", `body.s is "x", not Bool`, func(j JSON, typ string) error { _, err := As[bool](j, typ); return err }},
	} {
		if err := tt.as(get(tt.name), tt.typ); err == nil || err.Error() != tt.want {
			t.Errorf("%s as %s = %v, want %s", tt.name, tt.typ, err, tt.want)
		}
	}
}

func TestJSONLen(t *testing.T) {
	for in, want := range map[string]int64{`"héllo"`: 5, `[1,2,3]`: 3} {
		if n, err := JSONLen(jsonOf(t, in)); n != want || err != nil {
			t.Errorf("len(%s) = %d, %v", in, n, err)
		}
	}
	if _, err := JSONLen(jsonOf(t, `{"a":1}`)); err == nil || !strings.Contains(err.Error(), "len needs text or a list") {
		t.Errorf("len of an object = %v", err)
	}
}

func TestCompare(t *testing.T) {
	body := jsonOf(t, `{"price":"9.99","pages":412,"title":"Dune","ok":true,"none":null,"at":"2026-01-02T03:04:05Z","tags":["a"],"user":{"id":"x"}}`)
	f := func(name string) JSON {
		v, err := body.Field(name)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	type user struct {
		ID string `json:"id"`
	}
	tests := []struct {
		op   string
		a, b any
		want bool
	}{
		{"==", f("price"), 9.99, true},
		{"==", f("price"), Money(999), true},
		{"==", f("price"), int64(10), false},
		{"==", f("pages"), int64(412), true},
		{"==", f("pages"), 412.0, true},
		{"==", f("pages"), "412", true},
		{"==", f("title"), "Dune", true},
		{"==", f("title"), "dune", false},
		{"!=", f("title"), "Dune", false},
		{"==", f("ok"), true, true},
		{"==", f("ok"), "true", false},
		{"==", f("none"), nil, true},
		{"==", f("none"), (*string)(nil), true},
		{"==", f("title"), (*string)(nil), false},
		{"==", f("at"), time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), true},
		{"==", f("tags"), []string{"a"}, true},
		{"==", f("user"), &user{ID: "x"}, true},
		{"==", f("user"), &user{ID: "y"}, false},
		{"==", jsonOf(t, `"007"`), "7", false},
		{"==", Created, jsonOf(t, `"created"`), true},
		{">", f("price"), 9.5, true},
		{"<=", f("pages"), int64(412), true},
		{"<", f("pages"), "500", true},
		{"<", f("title"), "Earthsea", true},
		{">", f("at"), time.Date(2026, 1, 2, 3, 4, 5, 500, time.UTC), false},
		{"<", jsonOf(t, `"2026-01-02T03:04:05Z"`), jsonOf(t, `"2026-01-02T03:04:05.5Z"`), true},
		{"contains", f("title"), "UN", true},
		{"contains", "Dune Messiah", f("title"), true},
	}
	for _, tt := range tests {
		got, err := Compare(tt.op, tt.a, tt.b)
		if got != tt.want || err != nil {
			t.Errorf("%s %s %s = %v, %v; want %v", Show(tt.a), tt.op, Show(tt.b), got, err, tt.want)
		}
	}
	for _, tt := range []struct {
		op   string
		a, b any
		want string
	}{
		{"<", f("ok"), int64(1), "cannot compare true and 1 with <"},
		{"<", f("tags"), f("tags"), `cannot compare ["a"] and ["a"] with <`},
		{"contains", f("pages"), "4", `contains needs text on both sides, not 412 and "4"`},
	} {
		if _, err := Compare(tt.op, tt.a, tt.b); err == nil || err.Error() != tt.want {
			t.Errorf("%s %s %s = %v, want %s", Show(tt.a), tt.op, Show(tt.b), err, tt.want)
		}
	}
}

func TestShow(t *testing.T) {
	text := "Dune"
	price := Money(50)
	long := jsonOf(t, `"`+strings.Repeat("a", 400)+`"`)
	for _, tt := range []struct {
		v    any
		want string
	}{
		{"Dune", `"Dune"`},
		{"<b>", `"<b>"`},
		{int64(412), "412"},
		{9.5, "9.5"},
		{true, "true"},
		{Money(999), "9.99"},
		{&price, "0.50"},
		{&text, `"Dune"`},
		{(*string)(nil), "none"},
		{nil, "none"},
		{Conflict, "conflict"},
		{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "2026-01-02T03:04:05Z"},
		{jsonOf(t, `{"a": [1, "x"]}`), `{"a":[1,"x"]}`},
		{jsonOf(t, `null`), "none"},
		{jsonOf(t, ``), "none"},
		{[]string{"a"}, `["a"]`},
		{long, `"` + strings.Repeat("a", maxShow-1) + "…"},
	} {
		if got := Show(tt.v); got != tt.want {
			t.Errorf("Show(%#v) = %s, want %s", tt.v, got, tt.want)
		}
	}
}

func TestStrJSON(t *testing.T) {
	for in, want := range map[string]string{`"Dune"`: "Dune", `412`: "412", `null`: "none", `{"a":"<b>"}`: `{"a":"<b>"}`} {
		if got := Str(jsonOf(t, in)); got != want {
			t.Errorf("Str(%s) = %s, want %s", in, got, want)
		}
	}
	if got := PathSeg(jsonOf(t, `"a b"`)); got != "a%20b" {
		t.Errorf("PathSeg = %s", got)
	}
}

func TestFailures(t *testing.T) {
	invalid := &Error{Status: Invalid, Message: "Invalid input", Fields: map[string]string{"title": "must be at least 2 characters", "pages": "is required"}}
	f := asFailure(At(invalid, "main.vibe", 12))
	want := Failure{File: "main.vibe", Line: 12, Message: "invalid: Invalid input (pages is required; title must be at least 2 characters)"}
	if *f != want {
		t.Errorf("At(invalid) = %+v", f)
	}
	if f := asFailure(At(Missing("Book"), "main.vibe", 3)); f.Message != "not_found: Book not found" {
		t.Errorf("At(missing) = %+v", f)
	}
	if f := asFailure(At(Internalf("disk: %w", errors.New("full")), "main.vibe", 3)); f.Message != "internal: disk: full" {
		t.Errorf("At(internal) = %+v", f)
	}
	inner := At(errors.New("body has no field x"), "a.vibe", 4)
	if f := asFailure(At(inner, "main.vibe", 9)); f.File != "a.vibe" || f.Line != 4 {
		t.Errorf("At keeps the first location: %+v", f)
	}
	exp := ExpectFailed("main.vibe", 40, "res.status == conflict", OK, Conflict)
	if got := exp.Error(); got != "main.vibe:40: expect res.status == conflict\n  left:  ok\n  right: conflict" {
		t.Errorf("Error() = %q", got)
	}
	line := Report(exp)
	data, ok := strings.CutPrefix(line, FailurePrefix)
	if !ok {
		t.Fatalf("Report = %q", line)
	}
	var back Failure
	if err := json.Unmarshal([]byte(data), &back); err != nil || back != *asFailure(exp) {
		t.Errorf("Report = %s, %v", data, err)
	}
	if got := Report(Stop("main.vibe", 5, "no books")); !strings.Contains(got, `"message":"no books"`) || strings.Contains(got, "left") {
		t.Errorf("Report(Stop) = %s", got)
	}
}
