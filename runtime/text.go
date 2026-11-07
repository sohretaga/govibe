package vibe

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Len returns the number of characters in s, as len and Text min and max
// count them.
func Len(s string) int64 { return int64(utf8.RuneCountInString(s)) }

// Contains reports whether s contains sub, ignoring letter case, as
// GoVibe's contains does.
func Contains(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// SQLContains is the SQL function vibe_contains(s, sub), which queries
// use for contains. It is NULL when either side is NULL.
func SQLContains(args []driver.Value) (driver.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("vibe_contains takes 2 arguments, got %d", len(args))
	}
	s, ok1 := sqlText(args[0])
	sub, ok2 := sqlText(args[1])
	if !ok1 || !ok2 {
		return nil, nil
	}
	if Contains(s, sub) {
		return int64(1), nil
	}
	return int64(0), nil
}

func sqlText(v driver.Value) (string, bool) {
	switch v := v.(type) {
	case string:
		return v, true
	case []byte:
		return string(v), true
	}
	return "", false
}

// ValidEmail reports whether s looks like an email address: one @, a
// local part without spaces, and a domain of dot-separated labels.
func ValidEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || len(local) > 64 || strings.ContainsAny(local, " \t\r\n<>()[],;:\"\\") || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if r != '-' && !isLetterOrDigit(r) {
				return false
			}
		}
	}
	return true
}

func isLetterOrDigit(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r > utf8.RuneSelf
}

// Str turns a value into text, as str does: numbers in decimal, Money
// with two decimal places, times in RFC 3339, none as "none", and records
// and lists as JSON.
func Str(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case Money:
		return v.String()
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case nil:
		return "none"
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "none"
		}
		if rv.Elem().Kind() != reflect.Struct {
			return Str(rv.Elem().Interface())
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
