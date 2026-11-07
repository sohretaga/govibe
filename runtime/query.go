package vibe

import (
	"errors"
	"strconv"
	"time"
)

// Scanner reads the current row into dest.
type Scanner interface {
	Scan(dest ...any) error
}

// All runs a query and scans every row with scan. It never returns a nil
// slice, so an empty result is [] in JSON.
func All[T any](c *Ctx, query string, scan func(Scanner) (T, error), args ...any) (out []T, err error) {
	rows, err := c.Rows(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	out = []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// First runs a query for one row and returns nil when there is none.
func First[T any](c *Ctx, query string, scan func(Scanner) (*T, error), args ...any) (*T, error) {
	v, err := scan(c.Row(query, args...))
	if errors.Is(err, ErrNoRow) {
		return nil, nil
	}
	return v, err
}

// Find runs a query for one record of model and answers not_found when
// there is none.
func Find[T any](c *Ctx, query string, scan func(Scanner) (*T, error), model string, args ...any) (*T, error) {
	v, err := First(c, query, scan, args...)
	if err == nil && v == nil {
		return nil, Missing(model)
	}
	return v, err
}

// Count runs a SELECT COUNT(*) query.
func Count(c *Ctx, query string, args ...any) (int64, error) {
	var n int64
	err := c.Row(query, args...).Scan(&n)
	return n, err
}

// UniqueSlug makes a slug from text that taken reports free: the plain
// slug, then the slug with -2, -3 and so on. fallback is used when text
// has nothing a slug can spell.
func UniqueSlug(text, fallback string, taken func(string) (bool, error)) (string, error) {
	base := Slugify(text)
	if base == "" {
		base = fallback
	}
	for n := 1; ; n++ {
		s := base
		if n > 1 {
			suffix := "-" + strconv.Itoa(n)
			if len(s)+len(suffix) > maxSlug {
				s = trimDashes(s[:maxSlug-len(suffix)])
			}
			s += suffix
		}
		t, err := taken(s)
		if err != nil || !t {
			return s, err
		}
	}
}

func trimDashes(s string) string {
	for len(s) > 0 && s[len(s)-1] == '-' {
		s = s[:len(s)-1]
	}
	return s
}

// Now returns the current time in UTC, as now() does.
func Now() time.Time { return time.Now().UTC() }

// Ptr returns a pointer to v: an optional value that is not none.
func Ptr[T any](v T) *T { return &v }

// Deref returns *p, or the zero value when p is nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// OptDef reads an optional param with a default: the default when the
// param is missing, nil when it is null.
func OptDef[T any](in *Input, name string, k Kind, def T) *T {
	if !in.Has(name) {
		return &def
	}
	return Opt[T](in, name, k)
}

// EqPtr reports whether an optional value equals a value.
func EqPtr[T comparable](a *T, b T) bool { return a != nil && *a == b }

// EqPtrs reports whether two optional values are equal: both none, or
// both set to equal values.
func EqPtrs[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// WidenPtr turns an optional Int into an optional Float.
func WidenPtr(p *int64) *float64 {
	if p == nil {
		return nil
	}
	f := float64(*p)
	return &f
}

// WidenList turns a list of Int into a list of Float.
func WidenList(xs []int64) []float64 {
	out := make([]float64, len(xs))
	for i, x := range xs {
		out[i] = float64(x)
	}
	return out
}
