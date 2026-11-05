package vibe

import (
	"fmt"
	"time"
)

// timeLayout is how a Time is stored: RFC 3339 in UTC with a fixed
// number of fraction digits, so text order is time order.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// TimeArg returns t as the database stores it.
func TimeArg(t time.Time) string { return t.UTC().Format(timeLayout) }

// OptTimeArg returns t as the database stores it, or nil for none.
func OptTimeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return TimeArg(*t)
}

// ScanTime returns a destination for Rows.Scan that reads a stored Time
// into dst.
func ScanTime(dst *time.Time) any { return timeScanner{dst} }

// ScanOptTime returns a destination for Rows.Scan that reads a stored
// optional Time into dst, nil for NULL.
func ScanOptTime(dst **time.Time) any { return optTimeScanner{dst} }

type timeScanner struct{ dst *time.Time }

func (s timeScanner) Scan(src any) error {
	t, err := parseStored(src)
	if err != nil {
		return err
	}
	*s.dst = t
	return nil
}

type optTimeScanner struct{ dst **time.Time }

func (s optTimeScanner) Scan(src any) error {
	if src == nil {
		*s.dst = nil
		return nil
	}
	t, err := parseStored(src)
	if err != nil {
		return err
	}
	*s.dst = &t
	return nil
}

func parseStored(src any) (time.Time, error) {
	switch v := src.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return parseTimeText(v)
	case []byte:
		return parseTimeText(string(v))
	}
	return time.Time{}, fmt.Errorf("vibe: cannot read a time from %T", src)
}

func parseTimeText(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("vibe: stored time %q: %w", s, err)
	}
	return t.UTC(), nil
}

// Limit clamps the row count of a query to 0..1000, the default cap.
func Limit(n int64) int64 {
	return min(max(n, 0), maxRows)
}

// Offset clamps a query offset to at least 0.
func Offset(n int64) int64 { return max(n, 0) }

// maxRows is the most rows one query returns.
const maxRows = 1000
