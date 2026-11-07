package vibe

import (
	"errors"
	"fmt"
)

// Status is a response status name. GoVibe code uses the names; the
// runtime maps each one to its HTTP code.
type Status int

// Status names, in the order of the spec's table.
const (
	OK Status = iota
	Created
	Empty
	BadRequest
	LoginRequired
	Forbidden
	NotFound
	Conflict
	Invalid
	RateLimited
	Internal
)

var statuses = [...]struct {
	name    string
	code    int
	message string
}{
	OK:            {"ok", 200, ""},
	Created:       {"created", 201, ""},
	Empty:         {"empty", 204, ""},
	BadRequest:    {"bad_request", 400, "Bad request"},
	LoginRequired: {"login_required", 401, "Login required"},
	Forbidden:     {"forbidden", 403, "Forbidden"},
	NotFound:      {"not_found", 404, "Not found"},
	Conflict:      {"conflict", 409, "Conflict"},
	Invalid:       {"invalid", 422, "Invalid input"},
	RateLimited:   {"rate_limited", 429, "Too many requests"},
	Internal:      {"internal", 500, "Internal error"},
}

// String returns the status name, such as not_found.
func (s Status) String() string {
	if s < 0 || int(s) >= len(statuses) {
		return "internal"
	}
	return statuses[s].name
}

// HTTP returns the HTTP status code of s.
func (s Status) HTTP() int {
	if s < 0 || int(s) >= len(statuses) {
		return 500
	}
	return statuses[s].code
}

// Error is a failed request: a status, a message for the client and, for
// input errors, a message per field. Cause holds the error behind an
// internal failure; it is logged and never sent.
type Error struct {
	Status  Status
	Message string
	Fields  map[string]string
	Cause   error
	// Source is the .vibe file and line where a panic happened.
	Source string
}

// Error returns the status name and message.
func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Status.String() + ": " + e.Cause.Error()
	}
	return e.Status.String() + ": " + e.Message
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Cause }

// Fail returns an error that answers the request with status s and
// message, as fail does in GoVibe code. An extern fn returns it to fail
// like a route.
func Fail(s Status, message string) error {
	if message == "" && s >= 0 && int(s) < len(statuses) {
		message = statuses[s].message
	}
	return &Error{Status: s, Message: message}
}

// Missing returns the not_found error for a record of model that does not
// exist.
func Missing(model string) error {
	return &Error{Status: NotFound, Message: model + " not found"}
}

// Internalf returns an internal error whose cause is logged.
func Internalf(format string, args ...any) error {
	return &Error{Status: Internal, Message: statuses[Internal].message, Cause: fmt.Errorf(format, args...)}
}

// asError turns any error into an *Error. Errors that are not an *Error
// are internal.
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Status: Internal, Message: statuses[Internal].message, Cause: err}
}
