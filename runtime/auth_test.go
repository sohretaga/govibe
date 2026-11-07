package vibe

import (
	"bytes"
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTokens(t *testing.T) {
	a, ha := NewToken()
	b, hb := NewToken()
	if a == b || ha == hb || len(a) != 43 || len(ha) != 64 {
		t.Errorf("tokens %q %q, hashes %q %q", a, b, ha, hb)
	}
	if HashToken(a) != ha || strings.Contains(ha, a) {
		t.Error("HashToken does not match NewToken")
	}
}

func TestSessionToken(t *testing.T) {
	c := &Ctx{r: httptest.NewRequest("GET", "/", nil)}
	if c.SessionToken() != "" {
		t.Error("a request without a session has a token")
	}
	c.r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "from-cookie"})
	if got := c.SessionToken(); got != "from-cookie" {
		t.Errorf("cookie token = %q", got)
	}
	c.r.Header.Set("Authorization", "Bearer from-header")
	if got := c.SessionToken(); got != "from-header" {
		t.Errorf("bearer token = %q, want it to win over the cookie", got)
	}
}

func TestSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	c := &Ctx{w: w, r: httptest.NewRequest("GET", "/", nil)}
	c.SetSessionCookie("tok")
	got := w.Header().Get("Set-Cookie")
	for _, want := range []string{"vibe_session=tok", "Path=/", "HttpOnly", "SameSite=Lax", "Max-Age=2592000"} {
		if !strings.Contains(got, want) {
			t.Errorf("Set-Cookie %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "Secure") {
		t.Error("cookie over plain HTTP is Secure")
	}
	w = httptest.NewRecorder()
	c = &Ctx{w: w, r: httptest.NewRequest("GET", "/", nil)}
	c.r.TLS = &tls.ConnectionState{}
	c.SetSessionCookie("")
	if got := w.Header().Get("Set-Cookie"); !strings.Contains(got, "Max-Age=0") || !strings.Contains(got, "Secure") {
		t.Errorf("clearing cookie over TLS = %q", got)
	}
}

func TestNeedsRenewal(t *testing.T) {
	if NeedsRenewal(SessionExpiry()) {
		t.Error("a fresh session needs renewal")
	}
	if !NeedsRenewal(Now().Add(sessionLife - 25*time.Hour)) {
		t.Error("a session renewed over a day ago does not need renewal")
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(3)
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if !l.allow("ip", at.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("attempt %d refused", i+1)
		}
	}
	if l.allow("ip", at.Add(10*time.Second)) {
		t.Error("fourth attempt in a minute allowed")
	}
	if !l.allow("other", at.Add(10*time.Second)) {
		t.Error("another IP refused")
	}
	if !l.allow("ip", at.Add(61*time.Second)) {
		t.Error("attempt after a minute refused")
	}
	l.allow("late", at.Add(10*time.Minute))
	if _, ok := l.events["other"]; ok {
		t.Error("quiet clients are not forgotten")
	}
}

func TestLoginAttempt(t *testing.T) {
	s := &server{limit: newLimiter(loginLimit)}
	c := &Ctx{s: s, r: httptest.NewRequest("POST", "/auth/login", nil)}
	for i := 0; i < loginLimit; i++ {
		if err := c.LoginAttempt(); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if e := asError(c.LoginAttempt()); e.Status != RateLimited {
		t.Errorf("attempt 11 = %v, want rate_limited", e.Status)
	}
}

type fakeRow struct{ dest []any }

func (r *fakeRow) Scan(dest ...any) error { r.dest = dest; return nil }

func TestWithExtra(t *testing.T) {
	row := &fakeRow{}
	var a, b int
	if err := WithExtra(row, &b).Scan(&a); err != nil || len(row.dest) != 2 || row.dest[1] != &b {
		t.Errorf("WithExtra passed %v, %v", row.dest, err)
	}
}

func TestPanicSource(t *testing.T) {
	var logs bytes.Buffer
	s := &server{log: slog.New(slog.NewJSONHandler(&logs, nil)), ids: newIDs()}
	mux, err := s.routes([]Route{{"GET", "/div", divide}})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/div", nil))
	if w.Code != 500 || !strings.Contains(logs.String(), `books.vibe:12"`) {
		t.Errorf("panic = %d, logs %s", w.Code, logs.String())
	}
}

func TestSourceName(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u/shop/.vibe/build/books/a.vibe": "books/a.vibe",
		"vibeapp/main.vibe":                     "main.vibe",
		`C:\shop\.vibe\build\main.vibe`:         "main.vibe",
		"other.vibe":                            "other.vibe",
	} {
		if got := sourceName(in); got != want {
			t.Errorf("sourceName(%q) = %q, want %q", in, got, want)
		}
	}
}
