package build

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/sohretaga/govibe/internal/golden"
	"github.com/sohretaga/govibe/internal/project"
)

// goTool returns the go command, or skips the test: end-to-end tests
// compile real binaries.
func goTool(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("builds binaries; skipped with -short")
	}
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command on the PATH")
	}
	return path
}

// buildApp checks the project in src, builds it in a temporary root and
// returns the root and the binary.
func buildApp(t *testing.T, goCmd, src string) (root, bin string) {
	t.Helper()
	root = t.TempDir()
	srcs, err := project.Read(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if gos, err := filepath.Glob(filepath.Join(src, GoDir, "*.go")); err == nil {
		for _, g := range gos {
			data, err := os.ReadFile(g)
			if err != nil {
				t.Fatal(err)
			}
			srcs = append(srcs, project.Source{Name: GoDir + "/" + filepath.Base(g), Data: data})
		}
	}
	for _, s := range srcs {
		path := filepath.Join(root, filepath.FromSlash(s.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, s.Data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prog, diags := project.Check(slices.DeleteFunc(slices.Clone(srcs), func(s project.Source) bool { return strings.HasPrefix(s.Name, GoDir+"/") }))
	for _, d := range diags {
		t.Errorf("check: %v", d)
	}
	if prog == nil {
		t.FailNow()
	}
	bin = "app"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	diags, err = Build(ctx, prog, Options{Root: root, Output: bin, Go: goCmd})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diags {
		t.Fatalf("build: %s:%d:%d: %s %s\n%s", d.File, d.Range.Start.Line, d.Range.Start.Col, d.Code, d.Message, d.Snippet)
	}
	return root, filepath.Join(root, bin)
}

// TestGeneratedCodeVets builds every program in testdata/gen and runs go
// vet on the generated module.
func TestGeneratedCodeVets(t *testing.T) {
	goCmd := goTool(t)
	paths, err := filepath.Glob("../../testdata/gen/*.vibe")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no programs: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			dir := t.TempDir()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "main.vibe"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			root, _ := buildApp(t, goCmd, dir)
			cmd := exec.Command(goCmd, "vet", "./...")
			cmd.Dir = filepath.Join(root, filepath.FromSlash(Dir))
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go vet: %v\n%s", err, out)
			}
		})
	}
}

// server is a running app binary.
type server struct {
	base string
	cmd  *exec.Cmd
	logs *logBuffer
}

// logBuffer collects the app's log lines while the test reads them.
type logBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *logBuffer) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.WriteString(line + "\n")
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// start runs bin with a database in its folder and waits until it
// listens.
func start(t *testing.T, root, bin string) *server {
	t.Helper()
	cmd := exec.Command(bin, "--port", "0", "--db", "sqlite://"+filepath.ToSlash(filepath.Join(root, "test.db")))
	cmd.Dir = root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	s := &server{cmd: cmd, logs: &logBuffer{}}
	addr := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			s.logs.add(sc.Text())
			var line struct{ Msg, Addr string }
			if json.Unmarshal(sc.Bytes(), &line) == nil && line.Msg == "listening" {
				addr <- line.Addr
			}
		}
	}()
	t.Cleanup(func() {
		if runtime.GOOS == "windows" {
			_ = cmd.Process.Kill()
		} else {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		<-done
		if err := cmd.Wait(); err != nil && runtime.GOOS != "windows" {
			t.Errorf("app did not stop cleanly: %v\n%s", err, s.logs)
		}
	})
	select {
	case a := <-addr:
		s.base = "http://127.0.0.1:" + a[strings.LastIndex(a, ":")+1:]
	case <-done:
		t.Fatalf("app exited before listening:\n%s", s.logs)
	case <-time.After(10 * time.Second):
		t.Fatalf("app did not listen within 10s:\n%s", s.logs)
	}
	return s
}

// do sends a request and returns the status code and the decoded body.
func (s *server) do(t *testing.T, method, path, body string) (int, any) {
	t.Helper()
	code, v, _ := s.send(t, method, path, body, nil)
	return code, v
}

// send sends a request with headers and returns the status code, the
// decoded body and the response headers.
func (s *server) send(t *testing.T, method, path, body string, headers map[string]string) (int, any, http.Header) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, s.base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatalf("%s %s: body is not JSON: %q", method, path, data)
		}
	}
	if resp.StatusCode != 204 && resp.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("%s %s: Content-Type %q", method, path, resp.Header.Get("Content-Type"))
	}
	return resp.StatusCode, v, resp.Header
}

// get reads a path such as "error.fields.title" or "0.title" from a
// decoded JSON value.
func get(v any, path string) any {
	for _, key := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			v = x[key]
		case []any:
			i := 0
			for _, c := range key {
				i = i*10 + int(c-'0')
			}
			if i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// TestLibrary builds the reference app without auth and checks its crud
// and search routes over HTTP: the M3 acceptance test.
func TestLibrary(t *testing.T) {
	goCmd := goTool(t)
	root, bin := buildApp(t, goCmd, "../../testdata/e2e/library")
	s := start(t, root, bin)

	expect := func(method, path, body string, status int, want map[string]any) any {
		t.Helper()
		return s.expect(t, method, path, body, nil, status, want)
	}
	expect("GET", "/healthz", "", 200, map[string]any{"ok": true})
	user := expect("POST", "/users", `{"name":"Frank","email":"frank@example.com"}`, 201, map[string]any{"name": "Frank"})
	uid, _ := get(user, "id").(string)
	expect("POST", "/users", `{"name":"F","email":"nope"}`, 422, map[string]any{
		"error.code": "invalid", "error.fields.name": "must be at least 2 characters", "error.fields.email": "must be a valid email",
	})
	expect("POST", "/users", `{"name":"Frank Two","email":"frank@example.com"}`, 409, map[string]any{
		"error.code": "conflict", "error.fields.email": "is already taken",
	})

	dune := expect("POST", "/books", `{"title":"Dune","seller":"`+uid+`","pages":412,"price":9.99}`, 201, map[string]any{
		"title": "Dune", "price": "9.99", "published": false, "pages": 412.0, "seller": uid,
	})
	bid, _ := get(dune, "id").(string)
	expect("POST", "/books", `{"title":"Earthsea","seller":"`+uid+`","pages":183,"price":"7.50","published":true}`, 201, map[string]any{"price": "7.50"})
	expect("POST", "/books", `{"title":"X","seller":"0194a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5b","pages":0,"price":-1,"titel":"y","id":"z"}`, 422, map[string]any{
		"error.fields.title":  "must be at least 2 characters",
		"error.fields.pages":  "must be at least 1",
		"error.fields.price":  "must be at least 0.00",
		"error.fields.seller": "no User has this id",
		"error.fields.titel":  "unknown field",
		"error.fields.id":     "is not writable",
	})
	expect("POST", "/books", `{"title":"Dune","pages":"many","price":9.999}`, 400, map[string]any{
		"error.code": "bad_request", "error.fields.pages": "must be a whole number", "error.fields.price": "must have at most 2 decimal places",
	})
	expect("POST", "/books", `[1]`, 400, map[string]any{"error.code": "bad_request"})
	expect("POST", "/books", `{"title":"No pages","seller":"`+uid+`","price":1}`, 422, map[string]any{"error.fields.pages": "is required"})

	expect("GET", "/books", "", 200, map[string]any{"0.title": "Earthsea", "1.title": "Dune"})
	expect("GET", "/books?limit=1&offset=1", "", 200, map[string]any{"0.title": "Dune", "1.title": nil})
	expect("GET", "/books?limit=5000", "", 422, map[string]any{"error.fields.limit": "must be between 0 and 1000"})
	expect("GET", "/books/"+bid, "", 200, map[string]any{"title": "Dune"})
	expect("GET", "/books/not-an-id", "", 404, map[string]any{"error.code": "not_found", "error.message": "Book not found"})
	expect("GET", "/books/0194a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5b", "", 404, map[string]any{"error.code": "not_found"})

	// Search: optional q filters only when given, ignoring case; only
	// published books.
	expect("GET", "/books/search", "", 200, map[string]any{"0.title": "Earthsea", "1.title": nil})
	expect("GET", "/books/search?q=EARTH", "", 200, map[string]any{"0.title": "Earthsea"})
	expect("GET", "/books/search?q=dune", "", 200, map[string]any{"0.title": nil})
	expect("GET", "/books/search?limit=x", "", 400, map[string]any{"error.fields.limit": "must be a whole number"})

	expect("PATCH", "/books/"+bid, `{"published":true,"price":"12.50"}`, 200, map[string]any{"published": true, "price": "12.50", "title": "Dune"})
	expect("PATCH", "/books/"+bid, `{"title":null}`, 422, map[string]any{"error.fields.title": "is required"})
	expect("GET", "/books/search?q=dune", "", 200, map[string]any{"0.title": "Dune"})

	expect("DELETE", "/users/"+uid, "", 409, map[string]any{"error.code": "conflict"})
	if code, v := s.do(t, "DELETE", "/books/"+bid, ""); code != 204 || v != nil {
		t.Errorf("DELETE /books/%s = %d %v, want 204 and no body", bid, code, v)
	}
	expect("DELETE", "/books/"+bid, "", 404, map[string]any{"error.code": "not_found"})
	expect("PUT", "/books", "", 404, map[string]any{"error.code": "not_found"})

	if !strings.Contains(s.logs.String(), `"status":"conflict","code":409`) {
		t.Errorf("request log has no conflict line:\n%s", s.logs)
	}
}

// expect sends a request with the given headers, checks its status and
// the values at JSON paths, and returns the decoded body.
func (s *server) expect(t *testing.T, method, path, body string, headers map[string]string, status int, want map[string]any) any {
	t.Helper()
	code, v, _ := s.send(t, method, path, body, headers)
	if code != status {
		t.Errorf("%s %s %s = %d %v, want %d", method, path, body, code, v, status)
		return v
	}
	for p, w := range want {
		if got := get(v, p); got != w {
			t.Errorf("%s %s: %s = %#v, want %#v (body %v)", method, path, p, got, w, v)
		}
	}
	return v
}

// TestReferenceApp builds the reference app of the spec and checks auth
// and the owner rules over HTTP: the M4 acceptance test.
func TestReferenceApp(t *testing.T) {
	goCmd := goTool(t)
	root, bin := buildApp(t, goCmd, "../../examples/library")
	s := start(t, root, bin)
	type h = map[string]string
	type w = map[string]any
	bearer := func(token string) h { return h{"Authorization": "Bearer " + token} }

	s.expect(t, "POST", "/books", `{"title":"Dune","pages":412,"price":9.99}`, nil, 401, w{"error.code": "login_required"})
	s.expect(t, "GET", "/auth/me", "", nil, 401, w{"error.code": "login_required"})
	s.expect(t, "POST", "/auth/register", `{"email":"frank@example.com","password":"short","name":"F"}`, nil, 422, w{
		"error.fields.password": "must be at least 8 characters", "error.fields.name": "must be at least 2 characters",
	})
	s.expect(t, "POST", "/auth/register", `{"email":"frank@example.com","password":"long enough","name":"Frank","bio":"x"}`, nil, 422, w{
		"error.fields.bio": "unknown field",
	})

	// Register answers the user and a token, and sets the session cookie.
	code, reg, hdr := s.send(t, "POST", "/auth/register", `{"email":"frank@example.com","password":"long enough","name":"Frank"}`, nil)
	if code != 201 || get(reg, "user.name") != "Frank" || get(reg, "user.password_hash") != nil {
		t.Fatalf("register = %d %v", code, reg)
	}
	frank, _ := get(reg, "token").(string)
	frankID, _ := get(reg, "user.id").(string)
	cookie := hdr.Get("Set-Cookie")
	for _, part := range []string{"vibe_session=" + frank, "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(cookie, part) {
			t.Errorf("register Set-Cookie %q lacks %q", cookie, part)
		}
	}
	s.expect(t, "GET", "/auth/me", "", h{"Cookie": "vibe_session=" + frank}, 200, w{"name": "Frank", "email": "frank@example.com"})
	s.expect(t, "GET", "/auth/me", "", bearer(frank), 200, w{"id": frankID})
	s.expect(t, "POST", "/auth/register", `{"email":"frank@example.com","password":"long enough","name":"Frank"}`, nil, 409, w{
		"error.fields.email": "is already taken",
	})

	// Login: one answer for an unknown email and a wrong password.
	s.expect(t, "POST", "/auth/login", `{"email":"frank@example.com","password":"wrong password"}`, nil, 401, w{"error.message": "wrong email or password"})
	s.expect(t, "POST", "/auth/login", `{"email":"nobody@example.com","password":"wrong password"}`, nil, 401, w{"error.message": "wrong email or password"})
	s.expect(t, "POST", "/auth/register", `{"email":"ursula@example.com","password":"long enough","name":"Ursula"}`, nil, 201, nil)
	login := s.expect(t, "POST", "/auth/login", `{"email":"ursula@example.com","password":"long enough"}`, nil, 200, w{"user.name": "Ursula"})
	ursula, _ := get(login, "token").(string)

	// owner seller: creating needs login and sets seller from the session.
	s.expect(t, "POST", "/books", `{"title":"Dune","pages":412,"price":9.99,"seller":"x"}`, bearer(frank), 422, w{"error.fields.seller": "is not writable"})
	book := s.expect(t, "POST", "/books", `{"title":"Dune","pages":412,"price":9.99}`, bearer(frank), 201, w{"seller": frankID, "published": false})
	bid, _ := get(book, "id").(string)
	s.expect(t, "GET", "/books/"+bid, "", nil, 200, w{"title": "Dune"})
	s.expect(t, "PATCH", "/books/"+bid, `{"price":1}`, bearer(ursula), 403, w{"error.code": "forbidden"})
	s.expect(t, "DELETE", "/books/"+bid, "", bearer(ursula), 403, w{"error.code": "forbidden"})
	s.expect(t, "PATCH", "/books/"+bid, `{"price":"12.50"}`, bearer(frank), 200, w{"price": "12.50"})

	// The publish route uses me, so it needs login.
	s.expect(t, "POST", "/books/"+bid+"/publish", "", nil, 401, w{"error.code": "login_required"})
	s.expect(t, "POST", "/books/"+bid+"/publish", "", bearer(ursula), 403, w{"error.message": "not your book"})
	s.expect(t, "POST", "/books/"+bid+"/publish", "", bearer(frank), 200, w{"published": true})
	s.expect(t, "POST", "/books/"+bid+"/publish", "", bearer(frank), 409, w{"error.message": "already published"})
	s.expect(t, "GET", "/books/search?q=dune", "", nil, 200, w{"0.title": "Dune"})

	// Logout ends the session at once.
	s.expect(t, "POST", "/auth/logout", "", bearer(frank), 204, nil)
	s.expect(t, "GET", "/auth/me", "", bearer(frank), 401, w{"error.code": "login_required"})
	s.expect(t, "GET", "/auth/me", "", bearer(ursula), 200, w{"name": "Ursula"})

	// Login attempts are limited to 10 per minute per IP; this test made 3.
	for i := 3; i < 10; i++ {
		s.expect(t, "POST", "/auth/login", `{"email":"frank@example.com","password":"wrong password"}`, nil, 401, nil)
	}
	s.expect(t, "POST", "/auth/login", `{"email":"frank@example.com","password":"long enough"}`, nil, 429, w{"error.code": "rate_limited"})

	if logs := s.logs.String(); strings.Contains(logs, "long enough") || strings.Contains(logs, "$2a$") {
		t.Error("a password or its hash appears in the logs")
	}
}

// TestExternsAndPanics builds an app with extern fns and checks that
// they answer like the spec says, and that a panic is logged with its
// .vibe line.
func TestExternsAndPanics(t *testing.T) {
	goCmd := goTool(t)
	root, bin := buildApp(t, goCmd, "../../testdata/e2e/extern")
	s := start(t, root, bin)
	type w = map[string]any
	s.expect(t, "POST", "/pay", `{"amount":"12.50","token":"ok"}`, nil, 200, nil)
	if _, v := s.do(t, "POST", "/pay", `{"amount":"12.50","token":"ok"}`); v != "charged 12.50" {
		t.Errorf("pay = %v", v)
	}
	s.expect(t, "POST", "/pay", `{"amount":"1","token":"declined"}`, nil, 409, w{"error.message": "card declined"})
	s.expect(t, "POST", "/pay", `{"amount":"1","token":"boom"}`, nil, 500, w{"error.message": "Internal error"})
	book := s.expect(t, "POST", "/books", `{"title":"Dune","price":9.99}`, nil, 201, nil)
	id, _ := get(book, "id").(string)
	if _, v := s.do(t, "GET", "/books/"+id+"/describe", ""); v != "Dune costs 9.99" {
		t.Errorf("describe = %v", v)
	}
	s.expect(t, "GET", "/ratio?a=7&b=2", "", nil, 200, nil)
	s.expect(t, "GET", "/ratio?a=7&b=0", "", nil, 500, w{"error.code": "internal"})
	logs := s.logs.String()
	if !strings.Contains(logs, `"source":"main.vibe:13"`) {
		t.Errorf("the panic is not logged at main.vibe:13, the division in fn ratio:\n%s", logs)
	}
	if !strings.Contains(logs, "provider unreachable") {
		t.Errorf("the extern's error is not logged:\n%s", logs)
	}
}

// TestPrivateCrud checks crud owner private: every operation needs login,
// and others' records do not exist for a user.
func TestPrivateCrud(t *testing.T) {
	goCmd := goTool(t)
	dir := t.TempDir()
	data, err := os.ReadFile("../../testdata/gen/notes.vibe")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.vibe"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	root, bin := buildApp(t, goCmd, dir)
	s := start(t, root, bin)
	type w = map[string]any
	token := func(email string) map[string]string {
		v := s.expect(t, "POST", "/auth/register", `{"email":"`+email+`","password":"long enough","name":"Name"}`, nil, 201, nil)
		tok, _ := get(v, "token").(string)
		return map[string]string{"Authorization": "Bearer " + tok}
	}
	a, b := token("a@example.com"), token("b@example.com")
	s.expect(t, "GET", "/notes", "", nil, 401, w{"error.code": "login_required"})
	note := s.expect(t, "POST", "/notes", `{"text":"mine"}`, a, 201, w{"text": "mine"})
	id, _ := get(note, "id").(string)
	s.expect(t, "POST", "/notes", `{"text":"theirs"}`, b, 201, nil)
	s.expect(t, "GET", "/notes", "", a, 200, w{"0.text": "mine", "1.text": nil})
	s.expect(t, "GET", "/notes/"+id, "", b, 404, w{"error.code": "not_found"})
	s.expect(t, "PATCH", "/notes/"+id, `{"text":"x"}`, b, 404, w{"error.code": "not_found"})
	s.expect(t, "DELETE", "/notes/"+id, "", b, 404, w{"error.code": "not_found"})
	s.expect(t, "GET", "/notes/mine/count", "", a, 200, nil)
	s.expect(t, "GET", "/hello", "", a, 200, nil)
	if _, v := s.do(t, "GET", "/hello", ""); get(v, "error.code") != "login_required" {
		t.Errorf("GET /hello without a session = %v", v)
	}
	s.expect(t, "DELETE", "/notes/"+id, "", a, 204, nil)
	// owner without private: anyone reads, only the owner changes.
	tag := s.expect(t, "POST", "/tags", `{"label":"go"}`, a, 201, nil)
	tid, _ := get(tag, "id").(string)
	s.expect(t, "GET", "/tags/"+tid, "", nil, 200, w{"label": "go"})
	s.expect(t, "PATCH", "/tags/"+tid, `{"label":"x"}`, b, 403, w{"error.code": "forbidden"})
}
