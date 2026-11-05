package vibe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// serve sends one request through a server with the given routes.
func serve(t *testing.T, routes []Route, method, target, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var logs bytes.Buffer
	s := &server{log: slog.New(slog.NewJSONHandler(&logs, nil)), ids: newIDs()}
	mux, err := s.routes(routes)
	if err != nil {
		t.Fatal(err)
	}
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(method, target, rd))
	return w, logs.String()
}

func TestServerResponses(t *testing.T) {
	routes := []Route{
		{"GET", "/", func(c *Ctx) error { return c.OK("root") }},
		{"GET", "/ok", func(c *Ctx) error { return c.OK(map[string]any{"price": Money(999), "html": "<b>"}) }},
		{"POST", "/made", func(c *Ctx) error { return c.Created([]int{}) }},
		{"DELETE", "/gone", func(c *Ctx) error { return c.Empty() }},
		{"GET", "/fail", func(c *Ctx) error { return Fail(Conflict, "already published") }},
		{"GET", "/boom", func(c *Ctx) error { return errors.New("secret detail") }},
		{"GET", "/panic", func(c *Ctx) error { panic("bad index") }},
		{"GET", "/items/{id}", func(c *Ctx) error {
			id, err := c.PathID("id")
			if err != nil {
				return err
			}
			return c.OK(id)
		}},
	}
	tests := []struct {
		method, target string
		code           int
		body, log      string
	}{
		{"GET", "/", 200, `"root"`, `"status":"ok"`},
		{"GET", "/ok", 200, `{"html":"<b>","price":"9.99"}`, `"code":200`},
		{"POST", "/made", 201, `[]`, `"status":"created"`},
		{"DELETE", "/gone", 204, ``, `"status":"empty"`},
		{"GET", "/fail", 409, `{"error":{"code":"conflict","message":"already published"}}`, `"level":"INFO"`},
		{"GET", "/boom", 500, `{"error":{"code":"internal","message":"Internal error"}}`, `secret detail`},
		{"GET", "/panic", 500, `{"error":{"code":"internal","message":"Internal error"}}`, `panic: bad index`},
		{"GET", "/items/nope", 404, `{"error":{"code":"not_found","message":"Not found"}}`, `"status":"not_found"`},
		{"GET", "/items/0194a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5b", 200, `"0194a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5b"`, ``},
		{"GET", "/nowhere", 404, `{"error":{"code":"not_found","message":"no route for this method and path"}}`, ``},
		{"PUT", "/ok", 404, `"code":"not_found"`, ``},
		{"GET", "/ok/", 404, `"code":"not_found"`, ``},
	}
	for _, tt := range tests {
		w, logs := serve(t, routes, tt.method, tt.target, "")
		got := strings.TrimSpace(w.Body.String())
		if w.Code != tt.code || !strings.Contains(got, tt.body) || tt.body == "" && got != "" {
			t.Errorf("%s %s = %d %s, want %d %s", tt.method, tt.target, w.Code, got, tt.code, tt.body)
		}
		if !strings.Contains(logs, tt.log) {
			t.Errorf("%s %s logged %s, want %s", tt.method, tt.target, logs, tt.log)
		}
		if w.Header().Get("X-Request-Id") == "" {
			t.Errorf("%s %s has no X-Request-Id", tt.method, tt.target)
		}
		if strings.Contains(got, "secret") {
			t.Errorf("%s %s leaks the cause: %s", tt.method, tt.target, got)
		}
		if tt.code != 204 && w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%s %s Content-Type = %q", tt.method, tt.target, w.Header().Get("Content-Type"))
		}
	}
}

func TestConflictingRoutes(t *testing.T) {
	s := &server{log: slog.New(slog.DiscardHandler), ids: newIDs()}
	h := func(c *Ctx) error { return nil }
	_, err := s.routes([]Route{{"GET", "/a/{x}/b", h}, {"GET", "/a/b/{y}", h}})
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Errorf("routes = %v, want a conflict error", err)
	}
}

func TestRequestID(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-Id", "abc-123")
	if id := requestID(r); id != "abc-123" {
		t.Errorf("requestID = %q, want the client's", id)
	}
	r.Header.Set("X-Request-Id", "bad id\n")
	if id := requestID(r); id == "bad id\n" || len(id) != 16 {
		t.Errorf("requestID = %q, want a new one", id)
	}
}

type params struct {
	Title   string
	Pages   int64
	Price   Money
	Ratio   *float64
	Done    bool
	At      *time.Time
	Email   string
	Slug    *string
	Limit   int64
	Missing *string
}

func readParams(in *Input) params {
	return params{
		Title:   Req[string](in, "title", AsText),
		Pages:   Req[int64](in, "pages", AsInt),
		Price:   Req[Money](in, "price", AsMoney),
		Ratio:   Opt[float64](in, "ratio", AsFloat),
		Done:    Def(in, "done", AsBool, false),
		At:      Opt[time.Time](in, "at", AsTime),
		Email:   Def(in, "email", AsEmail, "a@b.co"),
		Slug:    Opt[string](in, "slug", AsSlug),
		Limit:   Def[int64](in, "limit", AsInt, 50),
		Missing: Opt[string](in, "missing", AsText),
	}
}

func bodyOf(t *testing.T, body string) (*Input, error) {
	t.Helper()
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	return bodyInput(httptest.NewRecorder(), r)
}

func TestBodyInput(t *testing.T) {
	in, err := bodyOf(t, `{"title":"Dune","pages":412,"price":9.99,"ratio":0.5,"done":true,"at":"2026-01-02T03:04:05+04:00","slug":null}`)
	if err != nil {
		t.Fatal(err)
	}
	p := readParams(in)
	if err := in.Err(); err != nil {
		t.Fatal(err)
	}
	if p.Title != "Dune" || p.Pages != 412 || p.Price != 999 || *p.Ratio != 0.5 || !p.Done || p.Limit != 50 || p.Slug != nil || p.Missing != nil || p.Email != "a@b.co" {
		t.Errorf("params = %+v", p)
	}
	if p.At.Location() != time.UTC || p.At.Hour() != 23 {
		t.Errorf("at = %v, want UTC", p.At)
	}
}

func TestQueryInput(t *testing.T) {
	r := httptest.NewRequest("GET", "/?title=Dune&pages=412&price=9.99&done=true&limit=5&extra=1", nil)
	in := queryInput(r)
	p := readParams(in)
	if err := in.Err(); err != nil {
		t.Fatal(err)
	}
	if p.Title != "Dune" || p.Pages != 412 || p.Price != 999 || !p.Done || p.Limit != 5 || p.Ratio != nil {
		t.Errorf("params = %+v", p)
	}
	if !in.Has("extra") || in.Has("nope") {
		t.Error("Has is wrong")
	}
}

func TestInputErrors(t *testing.T) {
	tests := []struct {
		body   string
		status Status
		fields map[string]string
	}{
		{`{"pages":1,"price":1}`, Invalid, map[string]string{"title": "is required"}},
		{`{"title":"x","pages":"many","price":1}`, BadRequest, map[string]string{"pages": "must be a whole number"}},
		{`{"title":"x","pages":1.5,"price":1}`, BadRequest, map[string]string{"pages": "must be a whole number"}},
		{`{"title":5,"pages":1,"price":1}`, BadRequest, map[string]string{"title": "must be text"}},
		{`{"title":"x","pages":1,"price":9.999}`, BadRequest, map[string]string{"price": "must have at most 2 decimal places"}},
		{`{"title":"x","pages":1,"price":true}`, BadRequest, map[string]string{"price": "must be an amount like 9.99"}},
		{`{"title":"x","pages":1,"price":1,"done":"yes"}`, BadRequest, map[string]string{"done": "must be true or false"}},
		{`{"title":"x","pages":1,"price":1,"at":"tomorrow"}`, BadRequest, map[string]string{"at": "must be a time like 2026-01-02T15:04:05Z"}},
		{`{"title":"x","pages":1,"price":1,"email":"nope"}`, Invalid, map[string]string{"email": "must be a valid email"}},
		{`{"title":"x","pages":1,"price":1,"slug":"Not A Slug"}`, Invalid, map[string]string{"slug": "must be lowercase letters, digits and dashes, like salam-dunya"}},
		{`{"title":"x","pages":1,"price":1,"titel":"y","id":"z"}`, Invalid, map[string]string{"titel": "unknown field", "id": "is not writable"}},
		{`{"title":null,"pages":"x","price":1}`, BadRequest, map[string]string{"pages": "must be a whole number"}},
	}
	for _, tt := range tests {
		in, err := bodyOf(t, tt.body)
		if err != nil {
			t.Fatal(err)
		}
		in.ReadOnly("id")
		readParams(in)
		e := asError(in.Err())
		if e.Status != tt.status || !equalMaps(e.Fields, tt.fields) {
			t.Errorf("%s: %v %v, want %v %v", tt.body, e.Status, e.Fields, tt.status, tt.fields)
		}
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestBadBodies(t *testing.T) {
	for _, body := range []string{`[1]`, `"x"`, `{"a":`, `null`, strings.Repeat(" ", maxBody) + "{}"} {
		_, err := bodyOf(t, body)
		if e := asError(err); err == nil || e.Status != BadRequest {
			t.Errorf("bodyInput(%.20q) = %v, want bad_request", body, err)
		}
	}
	in, err := bodyOf(t, "  ")
	if err != nil || in.Err() != nil {
		t.Errorf("empty body = %v", err)
	}
}

func TestErrorFields(t *testing.T) {
	w, _ := serve(t, []Route{{"POST", "/x", func(c *Ctx) error {
		in, err := c.Body()
		if err != nil {
			return err
		}
		Req[string](in, "title", AsText)
		return in.Err()
	}}}, "POST", "/x", `{}`)
	var got map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 422 || got["error"]["fields"].(map[string]any)["title"] != "is required" {
		t.Errorf("POST /x = %d %s", w.Code, w.Body)
	}
}

func TestConfig(t *testing.T) {
	a := &App{Name: "library", DB: "sqlite://library.db"}
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	cfg, err := a.config(nil, getenv, io.Discard)
	if err != nil || cfg.Port != 7080 || cfg.DB != "sqlite://library.db" {
		t.Errorf("defaults = %+v, %v", cfg, err)
	}
	env["VIBE_PORT"], env["VIBE_DB"] = "9000", "sqlite://env.db"
	cfg, err = a.config(nil, getenv, io.Discard)
	if err != nil || cfg.Port != 9000 || cfg.DB != "sqlite://env.db" {
		t.Errorf("env = %+v, %v", cfg, err)
	}
	cfg, err = a.config([]string{"--port", "9100", "--db", "sqlite://flag.db"}, getenv, io.Discard)
	if err != nil || cfg.Port != 9100 || cfg.DB != "sqlite://flag.db" {
		t.Errorf("flags = %+v, %v", cfg, err)
	}
	env["VIBE_PORT"] = "x"
	if _, err := a.config(nil, getenv, io.Discard); err == nil {
		t.Error("bad VIBE_PORT accepted")
	}
	delete(env, "VIBE_PORT")
	for _, args := range [][]string{{"--port", "70000"}, {"extra"}, {"--nope"}} {
		if _, err := a.config(args, getenv, io.Discard); err == nil {
			t.Errorf("config(%q) succeeded", args)
		}
	}
}

func TestUnknownStatusFail(t *testing.T) {
	w := httptest.NewRecorder()
	c := &Ctx{w: w, r: httptest.NewRequest("GET", "/", nil)}
	c.fail(&Error{Status: Internal, Message: "do not show"})
	if strings.Contains(w.Body.String(), "do not show") || w.Code != http.StatusInternalServerError {
		t.Errorf("fail(internal) = %d %s", w.Code, w.Body)
	}
}
