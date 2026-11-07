package vibe

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// Money is an amount in minor units: 9.99 is Money(999). JSON writes it as
// the string "9.99" and reads a number or a string.
type Money int64

var (
	errMoneyFormat = errors.New("must be an amount like 9.99")
	errMoneyCents  = errors.New("must have at most 2 decimal places")
)

// String returns m with two decimal places, such as "9.99" or "-0.50".
func (m Money) String() string {
	sign := ""
	u := uint64(m)
	if m < 0 {
		sign, u = "-", uint64(-m)
	}
	cents := u % 100
	s := strconv.FormatUint(u/100, 10) + "."
	if cents < 10 {
		s += "0"
	}
	return sign + s + strconv.FormatUint(cents, 10)
}

// ParseMoney reads an amount with at most two decimal places, such as
// "9.99", "-3.5" or "12".
func ParseMoney(s string) (Money, error) {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	whole, frac, dot := strings.Cut(s, ".")
	if whole == "" || dot && frac == "" || !digits(whole) || !digits(frac) {
		return 0, errMoneyFormat
	}
	if len(frac) > 2 {
		return 0, errMoneyCents
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > math.MaxInt64/100-1 {
		return 0, errMoneyFormat
	}
	for len(frac) < 2 {
		frac += "0"
	}
	c, _ := strconv.ParseInt(frac, 10, 64)
	v := w*100 + c
	if neg {
		v = -v
	}
	return Money(v), nil
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// MarshalJSON writes m as a string, so no client reads it as a float.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.String() + `"`), nil
}

// UnmarshalJSON reads a JSON number or string.
func (m *Money) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return errMoneyFormat
		}
	}
	v, err := ParseMoney(s)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
