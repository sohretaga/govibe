package vibe

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// SessionCookie is the cookie that carries the session token.
	SessionCookie = "vibe_session"
	// sessionLife is how long a session lasts after its last use.
	sessionLife = 30 * 24 * time.Hour
	// renewAfter is how old a renewal may get before a request renews the
	// session again, so an active session is written once a day at most.
	renewAfter = 24 * time.Hour
	// loginLimit is the number of login attempts one IP may make per
	// minute.
	loginLimit = 10
)

// Session is the answer of register and login: the account and the
// token that API clients send as Authorization: Bearer <token>.
type Session struct {
	User  any    `json:"user"`
	Token string `json:"token"`
}

// NewToken returns a new random session token and the hash the database
// stores.
func NewToken() (token, hash string) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("vibe: no random source: " + err.Error())
	}
	token = base64.RawURLEncoding.EncodeToString(b[:])
	return token, HashToken(token)
}

// HashToken returns the SHA-256 hash of a session token, in hex.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// SessionExpiry returns when a session started or renewed now expires.
func SessionExpiry() time.Time { return Now().Add(sessionLife) }

// NeedsRenewal reports whether a session that expires at expires was last
// renewed more than a day ago.
func NeedsRenewal(expires time.Time) bool {
	return expires.Before(Now().Add(sessionLife - renewAfter))
}

// SessionToken returns the session token of the request: from the
// Authorization: Bearer header, else from the session cookie, else "".
func (c *Ctx) SessionToken() string {
	if h := c.r.Header.Get("Authorization"); h != "" {
		if t, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(t)
		}
	}
	if ck, err := c.r.Cookie(SessionCookie); err == nil {
		return ck.Value
	}
	return ""
}

// HasSessionCookie reports whether the request carries the session
// cookie, which a renewed session sends again with its new expiry.
func (c *Ctx) HasSessionCookie() bool {
	_, err := c.r.Cookie(SessionCookie)
	return err == nil
}

// SetSessionCookie sends the session cookie: HttpOnly, SameSite=Lax, and
// Secure when the request came over TLS. An empty token clears it.
func (c *Ctx) SetSessionCookie(token string) {
	ck := &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.r.TLS != nil,
		MaxAge:   int(sessionLife / time.Second),
	}
	if token == "" {
		ck.MaxAge = -1
	}
	http.SetCookie(c.w, ck)
}

// NeedLogin is the error of a route that needs a session and has none.
func NeedLogin() error { return Fail(LoginRequired, "") }

// WithExtra returns a Scanner that reads the columns of row that a model
// scanner reads, then extra: the session query adds the expiry after the
// account's columns.
func WithExtra(row Scanner, extra ...any) Scanner { return extraScanner{row, extra} }

type extraScanner struct {
	row   Scanner
	extra []any
}

func (s extraScanner) Scan(dest ...any) error {
	return s.row.Scan(append(dest, s.extra...)...)
}

// LoginAttempt counts a login attempt from the client's IP and answers
// rate_limited after 10 attempts in a minute.
func (c *Ctx) LoginAttempt() error {
	ip, _, err := net.SplitHostPort(c.r.RemoteAddr)
	if err != nil {
		ip = c.r.RemoteAddr
	}
	if !c.s.limit.allow(ip, time.Now()) {
		return Fail(RateLimited, "too many login attempts; try again in a minute")
	}
	return nil
}

// limiter counts events per key in a sliding window of one minute.
type limiter struct {
	mu     sync.Mutex
	max    int
	events map[string][]time.Time
	swept  time.Time
}

func newLimiter(max int) *limiter {
	return &limiter{max: max, events: map[string][]time.Time{}}
}

// allow records an event for key at now and reports whether key stays
// within the limit.
func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	since := now.Add(-time.Minute)
	if now.Sub(l.swept) > time.Minute {
		// Forget clients that went quiet, so the map does not grow.
		for k, ts := range l.events {
			if len(ts) == 0 || ts[len(ts)-1].Before(since) {
				delete(l.events, k)
			}
		}
		l.swept = now
	}
	ts := l.events[key]
	i := 0
	for i < len(ts) && ts[i].Before(since) {
		i++
	}
	ts = ts[i:]
	if len(ts) >= l.max {
		l.events[key] = ts
		return false
	}
	l.events[key] = append(ts, now)
	return true
}

// NoAccountHash is a bcrypt hash that login checks when no account has
// the email, so a login takes as long whether the email exists or not.
const NoAccountHash = "$2a$10$WqIt4o3pbEQW4wmqr/i/DufKar4.kTFBGP1yDK9ScMccHePq2D.A2"

// CheckPassword adds the password rules a new password breaks to errs:
// at least 8 characters, and at most 72 bytes, the most bcrypt reads.
func CheckPassword(errs *Errors, password string) {
	switch {
	case Len(password) < 8:
		errs.Invalid("password", "must be at least 8 characters")
	case len(password) > 72:
		errs.Invalid("password", "must be at most 72 bytes")
	}
}
