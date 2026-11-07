package gogen

import (
	"fmt"
	"strings"

	"github.com/sohretaga/govibe/internal/ir"
	"github.com/sohretaga/govibe/internal/sqlgen"
)

// authHandler names the handler of an auth route: handleAuthLogin.
func authHandler(r *ir.Route) string { return "handleAuth" + pascal(r.Op) }

// auth returns auth.go: the session functions and the handlers of the
// four auth routes.
func (g *gen) auth() string {
	m := g.prog.Auth
	n := m.Name
	user := g.constSQL("sqlSessionUser", sqlgen.SessionUser(m), "loads the account of a live session.")
	insert := g.constSQL("sqlSessionInsert", sqlgen.SessionInsert, "starts a session.")
	renew := g.constSQL("sqlSessionRenew", sqlgen.SessionRenew, "renews a session.")
	del := g.constSQL("sqlSessionDelete", sqlgen.SessionDelete, "ends a session.")
	purge := g.constSQL("sqlSessionPurge", sqlgen.SessionPurge, "removes expired sessions.")
	byEmail := "sql" + n + "By" + g.fields[m.Field("email")]

	var b strings.Builder
	b.WriteString(header)
	fmt.Fprintf(&b, `// loadMe returns the %[1]s of the request's session, or answers
// login_required. It renews a session at most once a day.
func loadMe(c *vibe.Ctx) (*model.%[1]s, error) {
	token := c.SessionToken()
	if token == "" {
		return nil, vibe.NeedLogin()
	}
	hash := vibe.HashToken(token)
	var expires time.Time
	me, err := scan%[1]s(vibe.WithExtra(c.Row(%[2]s, hash, vibe.TimeArg(vibe.Now())), vibe.ScanTime(&expires)))
	if errors.Is(err, vibe.ErrNoRow) {
		return nil, vibe.NeedLogin()
	}
	if err != nil {
		return nil, err
	}
	if vibe.NeedsRenewal(expires) {
		if err := c.Exec(%[3]s, hash, vibe.TimeArg(vibe.SessionExpiry())); err != nil {
			return nil, err
		}
		if c.HasSessionCookie() {
			c.SetSessionCookie(token)
		}
	}
	return me, nil
}

// startSession starts a session for u, sets its cookie and returns its
// token. It first removes the sessions that expired.
func startSession(c *vibe.Ctx, u *model.%[1]s) (string, error) {
	if err := c.Exec(%[5]s, vibe.TimeArg(vibe.Now())); err != nil {
		return "", err
	}
	token, hash := vibe.NewToken()
	if err := c.Exec(%[4]s, hash, u.ID, vibe.TimeArg(vibe.SessionExpiry())); err != nil {
		return "", err
	}
	c.SetSessionCookie(token)
	return token, nil
}
`, n, user, renew, insert, purge)

	for _, r := range g.prog.Routes {
		if r.Origin != ir.FromAuth {
			continue
		}
		f := newBody(g)
		f.route = r
		switch r.Op {
		case "register":
			f.line("in, err := c.Body()")
			f.failOn()
			f.line(`in.ReadOnly("id", "created_at", "updated_at")`)
			f.line("r := &model.%s{}", n)
			f.line(`password := vibe.Req[string](in, "password", vibe.AsText)`)
			for _, p := range r.Params {
				if p.Name == "password" {
					continue
				}
				field := m.Field(p.Name)
				f.line("r.%s = %s", g.fields[field], f.read(withParamType(p, field.Type)))
			}
			f.line(`vibe.CheckPassword(&in.Errors, password)
in.Done()
if in.Errors.Err() == nil {
	// Hashing is slow, so it waits until the input is known to be good.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	r.PasswordHash = vibe.Ptr(string(hash))
}
r, err = insert%s(c, r, &in.Errors)
if err != nil {
	return err
}
token, err := startSession(c, r)
if err != nil {
	return err
}
return c.Created(vibe.Session{User: r, Token: token})`, n)
		case "login":
			f.line(`if err := c.LoginAttempt(); err != nil {
	return err
}
in, err := c.Body()
if err != nil {
	return err
}
email := vibe.Req[string](in, "email", vibe.AsText)
password := vibe.Req[string](in, "password", vibe.AsText)
if err := in.Err(); err != nil {
	return err
}
u, err := vibe.First(c, %s, scan%s, email)
if err != nil {
	return err
}
hash := vibe.NoAccountHash
if u != nil && u.PasswordHash != nil {
	hash = *u.PasswordHash
}
// The hash is checked even without an account, so the answer takes as
// long and reveals nothing about which emails have accounts.
if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || u == nil || u.PasswordHash == nil {
	return vibe.Fail(vibe.LoginRequired, "wrong email or password")
}
token, err := startSession(c, u)
if err != nil {
	return err
}
return c.OK(vibe.Session{User: u, Token: token})`, byEmail, n)
		case "logout":
			f.line(`in, err := c.Body()
if err != nil {
	return err
}
if err := in.Err(); err != nil {
	return err
}
if token := c.SessionToken(); token != "" {
	if err := c.Exec(%s, vibe.HashToken(token)); err != nil {
		return err
	}
}
c.SetSessionCookie("")
return c.Empty()`, del)
		case "me":
			f.line("me, err := loadMe(c)")
			f.failOn()
			f.line("return c.OK(me)")
		}
		name := authHandler(r)
		fmt.Fprintf(&b, "\n// %s answers %s %s, auth %s %s, declared at %s.\nfunc %s(c *vibe.Ctx) error {\n%s}\n",
			name, r.Method, r.Path, n, r.Op, where(r.Pos), name, f.w.String())
	}
	return b.String()
}
