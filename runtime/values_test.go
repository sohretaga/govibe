package vibe

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMoneyString(t *testing.T) {
	tests := []struct {
		m    Money
		want string
	}{
		{0, "0.00"}, {5, "0.05"}, {999, "9.99"}, {750, "7.50"}, {-50, "-0.50"}, {-1234, "-12.34"}, {100000, "1000.00"},
	}
	for _, tt := range tests {
		if got := tt.m.String(); got != tt.want {
			t.Errorf("Money(%d).String() = %q, want %q", int64(tt.m), got, tt.want)
		}
	}
}

func TestParseMoney(t *testing.T) {
	tests := []struct {
		in   string
		want Money
		err  string
	}{
		{"9.99", 999, ""},
		{"9.9", 990, ""},
		{"12", 1200, ""},
		{"0", 0, ""},
		{"-3.5", -350, ""},
		{"9.999", 0, "at most 2 decimal places"},
		{"", 0, "amount like"},
		{"9.", 0, "amount like"},
		{".5", 0, "amount like"},
		{"1e3", 0, "amount like"},
		{"abc", 0, "amount like"},
		{"+1", 0, "amount like"},
		{"99999999999999999999", 0, "amount like"},
	}
	for _, tt := range tests {
		got, err := ParseMoney(tt.in)
		switch {
		case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
			t.Errorf("ParseMoney(%q) error = %v, want %q", tt.in, err, tt.err)
		case tt.err == "" && (err != nil || got != tt.want):
			t.Errorf("ParseMoney(%q) = %d, %v, want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestMoneyJSON(t *testing.T) {
	b, err := json.Marshal(struct {
		P Money  `json:"p"`
		Q *Money `json:"q"`
	}{P: 999})
	if err != nil || string(b) != `{"p":"9.99","q":null}` {
		t.Fatalf("Marshal = %s, %v", b, err)
	}
	for _, in := range []string{`9.99`, `"9.99"`} {
		var m Money
		if err := json.Unmarshal([]byte(in), &m); err != nil || m != 999 {
			t.Errorf("Unmarshal(%s) = %d, %v", in, m, err)
		}
	}
	var m Money
	if err := json.Unmarshal([]byte(`true`), &m); err == nil {
		t.Error("Unmarshal(true) succeeded")
	}
}

func TestIDs(t *testing.T) {
	g := newIDs()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	g.now = func() time.Time { return at }
	first := g.next()
	if ts := fmt.Sprintf("%012x", at.UnixMilli()); strings.ReplaceAll(first, "-", "")[:12] != ts {
		t.Errorf("%q does not start with the timestamp %s", first, ts)
	}
	prev := first
	for i := 0; i < 5000; i++ {
		id := g.next()
		if !ValidID(id) {
			t.Fatalf("next() = %q, not a valid ID", id)
		}
		if id[14] != '7' {
			t.Fatalf("next() = %q, want version 7", id)
		}
		if v := id[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Fatalf("next() = %q, want RFC 9562 variant", id)
		}
		if id <= prev {
			t.Fatalf("next() = %q after %q, want increasing IDs within one millisecond", id, prev)
		}
		prev = id
	}
}

func TestValidID(t *testing.T) {
	for _, s := range []string{"0194a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5b", "00000000-0000-0000-0000-000000000000"} {
		if !ValidID(s) {
			t.Errorf("ValidID(%q) = false", s)
		}
	}
	for _, s := range []string{"", "abc", "0194A2B3-C4D5-7E6F-8A9B-0C1D2E3F4A5B", "0194a2b3c4d57e6f8a9b0c1d2e3f4a5b", "0194a2b3-c4d5-7e6f-8a9b-0c1d2e3f4a5g"} {
		if ValidID(s) {
			t.Errorf("ValidID(%q) = true", s)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Salam Dünya!", "salam-dunya"},
		{"Şəhər çiçəkləri ağ", "seher-cicekleri-ag"},
		{"İstanbul Işık", "istanbul-isik"},
		{"Привет, мир", "privet-mir"},
		{"Crème brûlée à Paris", "creme-brulee-a-paris"},
		{"Straße", "strasse"},
		{"  --Hello---World--  ", "hello-world"},
		{"Dünya", "dunya"},
		{"Go 1.27 is out", "go-1-27-is-out"},
		{"!!!", ""},
		{strings.Repeat("ab ", 40), strings.TrimSuffix(strings.Repeat("ab-", 27), "-")},
	}
	for _, tt := range tests {
		got := Slugify(tt.in)
		if got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if got != "" && !ValidSlug(got) {
			t.Errorf("Slugify(%q) = %q, not a valid slug", tt.in, got)
		}
		if len(got) > maxSlug {
			t.Errorf("Slugify(%q) has %d characters", tt.in, len(got))
		}
	}
}

func TestValidSlug(t *testing.T) {
	for _, s := range []string{"a", "salam-dunya", "post-2", "2026"} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false", s)
		}
	}
	for _, s := range []string{"", "-a", "a-", "a--b", "Salam", "a b", "ə"} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true", s)
		}
	}
}

func TestValidEmail(t *testing.T) {
	for _, s := range []string{"frank@example.com", "a.b+c@sub.example.az", "ali@bakı.az"} {
		if !ValidEmail(s) {
			t.Errorf("ValidEmail(%q) = false", s)
		}
	}
	for _, s := range []string{"", "frank", "frank@", "@example.com", "frank@example", "a b@example.com", "Frank <frank@example.com>", "a@-x.com", "a@x..com", "a@b@c.com"} {
		if ValidEmail(s) {
			t.Errorf("ValidEmail(%q) = true", s)
		}
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"Dune", "dune", true},
		{"Salam Dünya", "DÜNYA", true},
		{"Earthsea", "sea", true},
		{"Earthsea", "", true},
		{"Dune", "dunes", false},
	}
	for _, tt := range tests {
		if got := Contains(tt.s, tt.sub); got != tt.want {
			t.Errorf("Contains(%q, %q) = %v", tt.s, tt.sub, got)
		}
	}
	v, err := SQLContains([]driver.Value{"Salam Dünya", []byte("dünya")})
	if err != nil || v != int64(1) {
		t.Errorf("SQLContains = %v, %v", v, err)
	}
	if v, err := SQLContains([]driver.Value{nil, "x"}); err != nil || v != nil {
		t.Errorf("SQLContains(NULL) = %v, %v", v, err)
	}
	if _, err := SQLContains([]driver.Value{"x"}); err == nil {
		t.Error("SQLContains with one argument succeeded")
	}
}

func TestStr(t *testing.T) {
	s := "x"
	var none *string
	tests := []struct {
		v    any
		want string
	}{
		{int64(42), "42"},
		{2.5, "2.5"},
		{true, "true"},
		{Money(750), "7.50"},
		{"text", "text"},
		{time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "2026-01-02T03:04:05Z"},
		{&s, "x"},
		{none, "none"},
		{[]int64{1, 2}, "[1,2]"},
		{&struct {
			A string `json:"a"`
		}{"b"}, `{"a":"b"}`},
	}
	for _, tt := range tests {
		if got := Str(tt.v); got != tt.want {
			t.Errorf("Str(%#v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

func TestLen(t *testing.T) {
	if n := Len("Dünya"); n != 5 {
		t.Errorf("Len(Dünya) = %d, want 5", n)
	}
}

func TestStoredTime(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 6000, time.UTC)
	s := TimeArg(at)
	if s != "2026-01-02T03:04:05.000006000Z" {
		t.Fatalf("TimeArg = %q", s)
	}
	if TimeArg(at.Add(time.Second)) < s || TimeArg(at.Add(time.Nanosecond)) < s {
		t.Error("stored times do not sort as text")
	}
	var got time.Time
	if err := ScanTime(&got).(interface{ Scan(any) error }).Scan(s); err != nil || !got.Equal(at) {
		t.Errorf("ScanTime = %v, %v", got, err)
	}
	var opt *time.Time
	sc := ScanOptTime(&opt).(interface{ Scan(any) error })
	if err := sc.Scan(nil); err != nil || opt != nil {
		t.Errorf("ScanOptTime(nil) = %v, %v", opt, err)
	}
	if err := sc.Scan([]byte(s)); err != nil || opt == nil || !opt.Equal(at) {
		t.Errorf("ScanOptTime = %v, %v", opt, err)
	}
	if err := ScanTime(&got).(interface{ Scan(any) error }).Scan(int64(4)); err == nil {
		t.Error("ScanTime(int64) succeeded")
	}
	if OptTimeArg(nil) != nil || OptTimeArg(&at) != s {
		t.Error("OptTimeArg is wrong")
	}
}

func TestLimit(t *testing.T) {
	for _, tt := range []struct{ in, want int64 }{{-1, 0}, {0, 0}, {20, 20}, {1000, 1000}, {5000, 1000}} {
		if got := Limit(tt.in); got != tt.want {
			t.Errorf("Limit(%d) = %d", tt.in, got)
		}
	}
	if Offset(-5) != 0 || Offset(7) != 7 {
		t.Error("Offset is wrong")
	}
}

func TestDBPath(t *testing.T) {
	for in, want := range map[string]string{
		"sqlite://library.db":       "library.db",
		"sqlite:///data/library.db": "/data/library.db",
		"sqlite://:memory:":         ":memory:",
	} {
		if got, err := dbPath(in); err != nil || got != want {
			t.Errorf("dbPath(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "postgres://x", "sqlite://", "sqlite://a.db?x=1"} {
		if _, err := dbPath(in); err == nil {
			t.Errorf("dbPath(%q) succeeded", in)
		}
	}
}

func TestStatus(t *testing.T) {
	if Conflict.String() != "conflict" || Conflict.HTTP() != 409 || Empty.HTTP() != 204 {
		t.Error("status table is wrong")
	}
	if Status(99).String() != "internal" || Status(-1).HTTP() != 500 {
		t.Error("unknown status is not internal")
	}
	err := Fail(Forbidden, "")
	if e := asError(err); e.Status != Forbidden || e.Message != "Forbidden" {
		t.Errorf("Fail(Forbidden, \"\") = %+v", e)
	}
	if e := asError(Missing("Book")); e.Status != NotFound || e.Message != "Book not found" {
		t.Errorf("Missing = %+v", e)
	}
	if e := asError(Internalf("boom %d", 1)); e.Status != Internal || e.Error() != "internal: boom 1" {
		t.Errorf("Internalf = %+v", e)
	}
}

func TestMapErr(t *testing.T) {
	db := &DB{fields: map[string]string{"user.email": "email", "book.seller_id": "seller", "u.a": "a", "u.b": "b"}}
	tests := []struct {
		msg    string
		status Status
		fields map[string]string
	}{
		{"constraint failed: UNIQUE constraint failed: user.email (2067)", Conflict, map[string]string{"email": "is already taken"}},
		{"constraint failed: UNIQUE constraint failed: u.a, u.b (2067)", Conflict, map[string]string{"a": "is already taken", "b": "is already taken"}},
		{"constraint failed: FOREIGN KEY constraint failed (787)", Conflict, nil},
		{"disk I/O error", Internal, nil},
	}
	for _, tt := range tests {
		e := asError(db.mapErr(errors.New(tt.msg)))
		if e.Status != tt.status || len(e.Fields) != len(tt.fields) {
			t.Errorf("mapErr(%q) = %v %v, want %v %v", tt.msg, e.Status, e.Fields, tt.status, tt.fields)
			continue
		}
		for k, v := range tt.fields {
			if e.Fields[k] != v {
				t.Errorf("mapErr(%q) field %s = %q, want %q", tt.msg, k, e.Fields[k], v)
			}
		}
	}
}
