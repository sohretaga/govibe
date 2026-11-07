package vibe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
)

// Tester is one running GoVibe test: the app on a fresh in-memory
// database, served in-process, and the session that login_as started.
type Tester struct {
	db    *DB
	mux   http.Handler
	c     *Ctx
	ctx   context.Context
	token string
	log   bytes.Buffer
}

// Test runs one test of the app on a fresh in-memory database and
// returns why it failed, or nil. The test's code runs its own queries
// through c and sends requests through t.
func (a *App) Test(ctx context.Context, test func(t *Tester, c *Ctx) error) error {
	t, err := a.newTester(ctx)
	if err != nil {
		return &Failure{Message: "start the app: " + err.Error()}
	}
	err = errors.Join(t.run(test), t.db.Close())
	if err == nil {
		return nil
	}
	f := asFailure(err)
	if logged := t.appErrors(); logged != "" {
		f.Message += " (" + logged + ")"
	}
	return f
}

func (a *App) newTester(ctx context.Context) (*Tester, error) {
	db, err := openDB(ctx, "sqlite://:memory:", a.Tables)
	if err != nil {
		return nil, err
	}
	if err := syncSchema(ctx, db.write, a.Tables); err != nil {
		return nil, errors.Join(fmt.Errorf("schema sync: %w", err), db.Close())
	}
	if err := db.prepare(ctx, a.Queries); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	t := &Tester{db: db, ctx: ctx}
	s := &server{db: db, log: slog.New(slog.NewJSONHandler(&t.log, nil)), ids: newIDs(), limit: newLimiter(loginLimit)}
	t.mux, err = s.routes(a.Routes)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	t.c = &Ctx{w: newRecorder(), r: r, s: s, id: "test"}
	return t, nil
}

// run runs the test's code and turns a panic into a failure at the .vibe
// line where it happened.
func (t *Tester) run(test func(*Tester, *Ctx) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			f := &Failure{Message: fmt.Sprintf("panic: %v", r)}
			if src := panicSource(); src != "" {
				f.File, f.Line = splitSource(src)
			}
			err = f
		}
	}()
	return test(t, t.c)
}

// appErrors returns the internal errors the app logged while the test
// ran, which explain a response that answered internal.
func (t *Tester) appErrors() string {
	var out []string
	for _, line := range strings.Split(t.log.String(), "\n") {
		var rec struct {
			Level  string `json:"level"`
			Error  string `json:"error"`
			Source string `json:"source"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil || rec.Level != "ERROR" {
			continue
		}
		msg, _, _ := strings.Cut(strings.TrimPrefix(rec.Error, "internal: "), "\n")
		at := ""
		if rec.Source != "" {
			at = " at " + rec.Source
		}
		out = append(out, "the app answered internal"+at+": "+msg)
	}
	return strings.Join(out, "; ")
}

// LoginAs makes the requests that follow run with the session token, as
// login_as does.
func (t *Tester) LoginAs(token string) { t.token = token }

// Param is a named value of a request in a test.
type Param struct {
	Name  string
	Value any
}

// With returns a param of a request.
func With(name string, v any) Param { return Param{Name: name, Value: v} }

// Do sends a request to the app. Params go to the query string of a GET
// or DELETE and to the JSON body otherwise.
func (t *Tester) Do(method, path string, params ...Param) (*Response, error) {
	var body io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		q := url.Values{}
		for _, p := range params {
			for _, v := range queryValues(p.Value) {
				q.Add(p.Name, v)
			}
		}
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
	} else {
		b, err := bodyJSON(params)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequestWithContext(t.ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	if r.Body == nil {
		r.Body = http.NoBody
	}
	r.RemoteAddr = "192.0.2.1:1234"
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	w := newRecorder()
	t.mux.ServeHTTP(w, r)
	return w.response(), nil
}

// queryValues returns the query string values of a param: one per list
// element, none for none.
func queryValues(v any) []string {
	switch v := v.(type) {
	case nil:
		return nil
	case JSON:
		if list, ok := v.v.([]any); ok {
			out := make([]string, len(list))
			for i, x := range list {
				out[i] = JSON{v: x}.text()
			}
			return out
		}
		if v.v == nil {
			return nil
		}
		return []string{v.text()}
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		if rv.Elem().Kind() != reflect.Struct {
			return queryValues(rv.Elem().Interface())
		}
	case reflect.Slice:
		var out []string
		for i := range rv.Len() {
			out = append(out, queryValues(rv.Index(i).Interface())...)
		}
		return out
	}
	return []string{Str(v)}
}

// bodyJSON returns the JSON object of params, in their order.
func bodyJSON(params []Param) ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, p := range params {
		if i > 0 {
			b.WriteByte(',')
		}
		name, err := marshal(p.Name)
		if err != nil {
			return nil, err
		}
		v, err := marshal(p.Value)
		if err != nil {
			return nil, fmt.Errorf("param %s: %w", p.Name, err)
		}
		b.Write(bytes.TrimSpace(name))
		b.WriteByte(':')
		b.Write(bytes.TrimSpace(v))
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// PathSeg returns a value interpolated into the path of a request.
func PathSeg(v any) string { return url.PathEscape(Str(v)) }

// Response is the answer to a request in a test.
type Response struct {
	Status Status
	Body   JSON
}

// recorder is the http.ResponseWriter of a request in a test.
type recorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func newRecorder() *recorder { return &recorder{header: http.Header{}} }

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.WriteHeader(http.StatusOK)
	return r.body.Write(b)
}

func (r *recorder) response() *Response {
	code := r.code
	if code == 0 {
		code = http.StatusOK
	}
	status := Internal
	for i, s := range statuses {
		if s.code == code {
			status = Status(i)
		}
	}
	return &Response{Status: status, Body: decodeJSON(r.body.Bytes(), "body")}
}

// Failure is why a test failed: the .vibe line, a message and, for a
// failed expect, its two sides as GoVibe writes values.
type Failure struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	Left    string `json:"left,omitempty"`
	Right   string `json:"right,omitempty"`
}

func (f *Failure) Error() string {
	var b strings.Builder
	if f.File != "" {
		fmt.Fprintf(&b, "%s:%d: ", f.File, f.Line)
	}
	b.WriteString(f.Message)
	if f.Left != "" || f.Right != "" {
		fmt.Fprintf(&b, "\n  left:  %s\n  right: %s", f.Left, f.Right)
	}
	return b.String()
}

// FailurePrefix starts the line a failed test prints for vibe test.
const FailurePrefix = "vibe:failure "

// Report returns the line a failed test prints: FailurePrefix and the
// failure as JSON.
func Report(err error) string {
	b, jerr := json.Marshal(asFailure(err))
	if jerr != nil {
		return FailurePrefix + `{"message":"cannot report the failure"}`
	}
	return FailurePrefix + string(b)
}

func asFailure(err error) *Failure {
	var f *Failure
	if errors.As(err, &f) {
		return f
	}
	return &Failure{Message: describe(err)}
}

// At returns err as the failure of the test step at file:line. A step
// that already failed with a location keeps it.
func At(err error, file string, line int) error {
	var f *Failure
	if errors.As(err, &f) {
		if f.File == "" {
			f.File, f.Line = file, line
		}
		return f
	}
	return &Failure{File: file, Line: line, Message: describe(err)}
}

// Stop returns the failure of a fail statement in a test.
func Stop(file string, line int, message string) error {
	return &Failure{File: file, Line: line, Message: message}
}

// ExpectFailed returns the failure of a false expect: its source text and
// the values of its two sides.
func ExpectFailed(file string, line int, text string, left, right any) error {
	return &Failure{File: file, Line: line, Message: "expect " + text, Left: Show(left), Right: Show(right)}
}

// describe returns an error as a test failure shows it: the status, the
// message and the problem of each field.
func describe(err error) string {
	var e *Error
	if !errors.As(err, &e) {
		return err.Error()
	}
	msg := e.Status.String() + ": " + e.Message
	if e.Status == Internal && e.Cause != nil {
		msg = "internal: " + e.Cause.Error()
	}
	if len(e.Fields) > 0 {
		names := make([]string, 0, len(e.Fields))
		for name := range e.Fields {
			names = append(names, name)
		}
		slices.Sort(names)
		parts := make([]string, len(names))
		for i, name := range names {
			parts[i] = name + " " + e.Fields[name]
		}
		msg += " (" + strings.Join(parts, "; ") + ")"
	}
	return msg
}

// splitSource splits "main.vibe:12" into its file and line.
func splitSource(src string) (string, int) {
	i := strings.LastIndexByte(src, ':')
	if i < 0 {
		return src, 0
	}
	var line int
	if _, err := fmt.Sscan(src[i+1:], &line); err != nil {
		return src, 0
	}
	return src[:i], line
}
