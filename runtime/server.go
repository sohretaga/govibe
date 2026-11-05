package vibe

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"
)

// defaultPort is the port an app serves on without --port or VIBE_PORT.
const defaultPort = 7080

// App is a built GoVibe app: its tables, the SQL of every query and its
// routes. The generated main function fills it in and calls Main.
type App struct {
	Name    string
	DB      string // database URL from the app block
	Tables  []Table
	Queries []string
	Routes  []Route
}

// Route is one HTTP endpoint.
type Route struct {
	Method string // GET, POST, PATCH or DELETE
	Path   string // a net/http pattern path, such as /books/{id}
	Handle func(*Ctx) error
}

// Config holds the settings an app starts with.
type Config struct {
	Port int
	DB   string
}

// Main runs the app until SIGINT or SIGTERM and returns the exit code.
// Flags override environment variables, which override the app block:
// --port or VIBE_PORT (default 7080) and --db or VIBE_DB.
func (a *App) Main(args []string) int {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := a.config(args, os.Getenv, os.Stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := a.Run(ctx, cfg, log, nil); err != nil {
		log.Error("stopped", "error", err.Error())
		return 1
	}
	return 0
}

func (a *App) config(args []string, getenv func(string) string, usage io.Writer) (Config, error) {
	cfg := Config{Port: defaultPort, DB: a.DB}
	if v := getenv("VIBE_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("VIBE_PORT=%q: want a port number", v)
		}
		cfg.Port = p
	}
	if v := getenv("VIBE_DB"); v != "" {
		cfg.DB = v
	}
	fs := flag.NewFlagSet(a.Name, flag.ContinueOnError)
	fs.SetOutput(usage)
	fs.IntVar(&cfg.Port, "port", cfg.Port, "port to serve on (env VIBE_PORT)")
	fs.StringVar(&cfg.DB, "db", cfg.DB, "database URL, such as sqlite://app.db (env VIBE_DB)")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if cfg.Port < 0 || cfg.Port > 65535 {
		return cfg, fmt.Errorf("port %d is out of range", cfg.Port)
	}
	return cfg, nil
}

// Run opens the database, syncs the schema and serves until ctx is done.
// It calls ready, when not nil, with the address it listens on.
func (a *App) Run(ctx context.Context, cfg Config, log *slog.Logger, ready func(addr string)) (err error) {
	db, err := openDB(ctx, cfg.DB, a.Tables)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err := syncSchema(ctx, db.write, a.Tables); err != nil {
		return fmt.Errorf("schema sync: %w", err)
	}
	if err := db.prepare(ctx, a.Queries); err != nil {
		return err
	}
	s := &server{db: db, log: log, ids: newIDs()}
	mux, err := s.routes(a.Routes)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	log.Info("listening", "app", a.Name, "addr", ln.Addr().String(), "db", cfg.DB)
	if ready != nil {
		ready(ln.Addr().String())
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		return err
	}
	log.Info("stopped", "app", a.Name)
	return nil
}

// server is what every request shares.
type server struct {
	db  *DB
	log *slog.Logger
	ids *ids
}

func (s *server) routes(routes []Route) (mux *http.ServeMux, err error) {
	mux = http.NewServeMux()
	defer func() {
		// ServeMux panics on patterns that conflict.
		if r := recover(); r != nil {
			err = fmt.Errorf("routes: %v", r)
		}
	}()
	for _, rt := range routes {
		path := rt.Path
		if path == "/" {
			path = "/{$}"
		}
		mux.Handle(rt.Method+" "+path, s.handler(rt.Handle))
	}
	mux.Handle("/", s.handler(func(*Ctx) error { return Fail(NotFound, "no route for this method and path") }))
	return mux, nil
}

func (s *server) handler(h func(*Ctx) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		c := &Ctx{w: w, r: r, s: s, id: requestID(r)}
		w.Header().Set("X-Request-Id", c.id)
		err := c.call(h)
		if err != nil && !c.written {
			c.fail(asError(err))
		}
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", c.status.String(),
			"code", c.status.HTTP(),
			"duration_ms", float64(time.Since(start).Microseconds()) / 1000,
			"request_id", c.id,
		}
		level := slog.LevelInfo
		if e := asError(err); err != nil && e.Status == Internal {
			level = slog.LevelError
			attrs = append(attrs, "error", e.Error())
		}
		if c.writeErr != nil {
			attrs = append(attrs, "write_error", c.writeErr.Error())
		}
		s.log.Log(r.Context(), level, "request", attrs...)
	})
}

// requestID returns the request's X-Request-Id when it is short and plain,
// or a new random one.
func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-Id"); id != "" && len(id) <= 64 && plain(id) {
		return id
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}

func plain(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// Ctx is one request as generated handlers see it.
type Ctx struct {
	w        http.ResponseWriter
	r        *http.Request
	s        *server
	id       string
	written  bool
	status   Status
	writeErr error
}

// call runs h and turns a panic into an internal error with its stack.
func (c *Ctx) call(h func(*Ctx) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &Error{Status: Internal, Message: statuses[Internal].message, Cause: fmt.Errorf("panic: %v\n%s", r, debug.Stack())}
		}
	}()
	return h(c)
}

// Context returns the request's context.
func (c *Ctx) Context() context.Context { return c.r.Context() }

// Request returns the HTTP request.
func (c *Ctx) Request() *http.Request { return c.r }

// NewID returns a new UUIDv7.
func (c *Ctx) NewID() string { return c.s.ids.next() }

// Query returns the params in the query string.
func (c *Ctx) Query() *Input { return queryInput(c.r) }

// Body returns the params in the JSON body; a body that is not a JSON
// object answers bad_request.
func (c *Ctx) Body() (*Input, error) { return bodyInput(c.w, c.r) }

// PathText returns a path parameter.
func (c *Ctx) PathText(name string) string { return c.r.PathValue(name) }

// PathID returns a path parameter that holds an ID. A value that is not
// an ID answers not_found, like a missing record.
func (c *Ctx) PathID(name string) (string, error) {
	v := c.r.PathValue(name)
	if !ValidID(v) {
		return "", Fail(NotFound, "")
	}
	return v, nil
}

// Rows runs a query that returns rows.
func (c *Ctx) Rows(query string, args ...any) (*Rows, error) {
	rows, err := c.s.db.Rows(c.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	return &Rows{rows: rows}, nil
}

// Row runs a query that returns at most one row.
func (c *Ctx) Row(query string, args ...any) *Row {
	return &Row{row: c.s.db.Row(c.Context(), query, args...), db: c.s.db}
}

// Exec runs a statement that changes the database.
func (c *Ctx) Exec(query string, args ...any) error {
	return c.s.db.Exec(c.Context(), query, args...)
}

// Exists reports whether a query finds a row.
func (c *Ctx) Exists(query string, args ...any) (bool, error) {
	return c.s.db.Exists(c.Context(), query, args...)
}

// OK answers ok with v as JSON.
func (c *Ctx) OK(v any) error { return c.send(OK, v) }

// Created answers created with v as JSON.
func (c *Ctx) Created(v any) error { return c.send(Created, v) }

// Empty answers empty, with no body.
func (c *Ctx) Empty() error {
	c.w.WriteHeader(Empty.HTTP())
	c.written, c.status = true, Empty
	return nil
}

func (c *Ctx) send(s Status, v any) error {
	body, err := marshal(v)
	if err != nil {
		return Internalf("encode the response: %w", err)
	}
	c.write(s, body)
	return nil
}

func (c *Ctx) fail(e *Error) {
	type body struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields,omitempty"`
	}
	msg := e.Message
	if e.Status == Internal || msg == "" {
		msg = statuses[e.Status].message
	}
	out, err := marshal(map[string]body{"error": {Code: e.Status.String(), Message: msg, Fields: e.Fields}})
	if err != nil {
		out = []byte(`{"error":{"code":"internal","message":"Internal error"}}` + "\n")
	}
	c.write(e.Status, out)
}

func (c *Ctx) write(s Status, body []byte) {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.w.WriteHeader(s.HTTP())
	if _, err := c.w.Write(body); err != nil {
		c.writeErr = err
	}
	c.written, c.status = true, s
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Healthz answers {"ok":true} when the database answers a ping.
func Healthz(c *Ctx) error {
	if err := c.s.db.write.PingContext(c.Context()); err != nil {
		return Internalf("ping the database: %w", err)
	}
	return c.OK(map[string]bool{"ok": true})
}

// Rows is the result of a query for many rows.
type Rows struct{ rows *sql.Rows }

// Next prepares the next row.
func (r *Rows) Next() bool { return r.rows.Next() }

// Scan reads the current row.
func (r *Rows) Scan(dest ...any) error { return r.rows.Scan(dest...) }

// Close closes the rows and returns the first error of the iteration.
func (r *Rows) Close() error { return errors.Join(r.rows.Err(), r.rows.Close()) }

// Row is the result of a query for at most one row.
type Row struct {
	row *sql.Row
	db  *DB
}

// Scan reads the row. It answers ErrNoRow when there is none.
func (r *Row) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoRow
	}
	if err != nil {
		return r.db.mapErr(err)
	}
	return nil
}

// ErrNoRow means a query for one row found none.
var ErrNoRow = errors.New("vibe: no row")
