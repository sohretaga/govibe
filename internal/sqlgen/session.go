package sqlgen

import "github.com/sohretaga/govibe/internal/ir"

// SessionTable is the table of login sessions. It stores a SHA-256 hash of
// each token, never the token, so a copy of the database cannot be used
// to log in.
const SessionTable = "vibe_session"

func sessionTable(auth *ir.Model) Table {
	t := Table{Name: SessionTable}
	cols := []Column{
		{Name: "token_hash", Type: "TEXT", NotNull: true},
		{Name: "user_id", Type: "TEXT", NotNull: true},
		{Name: "expires_at", Type: "TEXT", NotNull: true},
	}
	defs := []string{
		"token_hash TEXT NOT NULL PRIMARY KEY",
		"user_id TEXT NOT NULL REFERENCES " + ident(auth.Table) + " (id) ON DELETE CASCADE",
		"expires_at TEXT NOT NULL",
	}
	t.Columns = cols
	t.Create = "CREATE TABLE " + SessionTable + " (" + defs[0] + ", " + defs[1] + ", " + defs[2] + ") STRICT"
	t.Indexes = []string{"CREATE INDEX IF NOT EXISTS " + SessionTable + "_user_id_idx ON " + SessionTable + " (user_id)"}
	return t
}

// SessionInsert stores a session: ?1 the token hash, ?2 the user id, ?3
// when it expires.
const SessionInsert = "INSERT INTO " + SessionTable + " (token_hash, user_id, expires_at) VALUES (?1, ?2, ?3)"

// SessionRenew moves the expiry of session ?1 to ?2.
const SessionRenew = "UPDATE " + SessionTable + " SET expires_at = ?2 WHERE token_hash = ?1"

// SessionDelete ends session ?1.
const SessionDelete = "DELETE FROM " + SessionTable + " WHERE token_hash = ?1"

// SessionPurge removes the sessions that expired before ?1.
const SessionPurge = "DELETE FROM " + SessionTable + " WHERE expires_at < ?1"

// SessionUser returns the statement that loads the account of session ?1
// when it has not expired at ?2: the columns of the auth model, then the
// session's expiry.
func SessionUser(auth *ir.Model) string {
	return "SELECT " + columnList(auth, "u.") + ", s.expires_at FROM " + ident(auth.Table) + " AS u JOIN " + SessionTable +
		" AS s ON s.user_id = u.id WHERE s.token_hash = ?1 AND s.expires_at > ?2"
}
