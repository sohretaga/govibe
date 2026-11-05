package gogen

import (
	"go/token"
	"strconv"
	"strings"
)

// pascal turns a snake_case name into a Go exported name: title becomes
// Title, created_at CreatedAt, and id and a trailing _id become ID.
func pascal(name string) string {
	var b strings.Builder
	for _, part := range strings.Split(name, "_") {
		if part == "" {
			continue
		}
		if part == "id" {
			b.WriteString("ID")
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

// reserved are the names a GoVibe local cannot keep in Go: the names
// generated code declares itself and the packages it imports. Go keywords
// and predeclared identifiers are added by isReserved.
var reserved = map[string]bool{
	"c": true, "in": true, "err": true, "errs": true, "r": true, "now": true,
	"vibe": true, "time": true, "strings": true, "errors": true, "os": true, "sqlite": true, "driver": true,
}

var predeclared = map[string]bool{
	"any": true, "append": true, "bool": true, "byte": true, "cap": true, "clear": true, "close": true,
	"comparable": true, "complex": true, "complex64": true, "complex128": true, "copy": true, "delete": true,
	"error": true, "false": true, "float32": true, "float64": true, "imag": true, "int": true, "int8": true,
	"int16": true, "int32": true, "int64": true, "iota": true, "len": true, "make": true, "max": true,
	"min": true, "new": true, "nil": true, "panic": true, "print": true, "println": true, "real": true,
	"recover": true, "rune": true, "string": true, "true": true, "uint": true, "uint8": true, "uint16": true,
	"uint32": true, "uint64": true, "uintptr": true,
}

// isReserved reports whether a local named name would clash in Go. Names
// of the form t1, t2, ... are kept for temporaries.
func isReserved(name string) bool {
	if reserved[name] || predeclared[name] || token.IsKeyword(name) {
		return true
	}
	if len(name) > 1 && name[0] == 't' {
		if _, err := strconv.Atoi(name[1:]); err == nil {
			return true
		}
	}
	return false
}
