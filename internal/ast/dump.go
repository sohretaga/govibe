package ast

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/sohretaga/govibe/internal/token"
)

// Dump returns a readable tree of n without positions. Golden files and
// the formatter's round-trip tests compare trees through it, so two trees
// with the same Dump are the same program with the same comments.
//
// Each node prints as its type name followed by its scalar fields; child
// nodes follow, one per line and indented. Zero fields are left out.
func Dump(n any) string {
	var b strings.Builder
	dumpValue(&b, reflect.ValueOf(n), 0)
	return b.String()
}

var (
	posType  = reflect.TypeFor[token.Pos]()
	fileType = reflect.TypeFor[*token.File]()
	kindType = reflect.TypeFor[token.Kind]()
)

func dumpValue(b *strings.Builder, v reflect.Value, depth int) {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		v = v.Elem()
	}
	if !v.IsValid() {
		b.WriteString("nil\n")
		return
	}
	t := v.Type()
	b.WriteString(t.Name())
	if t.Kind() != reflect.Struct {
		b.WriteByte('\n')
		return
	}
	for i := range t.NumField() {
		if s, ok := scalar(t.Field(i), v.Field(i)); ok {
			b.WriteString(" " + t.Field(i).Name + "=" + s)
		}
	}
	b.WriteByte('\n')
	indent := strings.Repeat("  ", depth+1)
	for i := range t.NumField() {
		ft, fv := t.Field(i), v.Field(i)
		if _, ok := scalar(ft, fv); ok || ft.Type == posType || ft.Type == fileType || fv.IsZero() {
			continue
		}
		if fv.Kind() == reflect.Slice {
			for j := range fv.Len() {
				b.WriteString(indent + ft.Name + "[" + strconv.Itoa(j) + "]: ")
				dumpValue(b, fv.Index(j), depth+1)
			}
			continue
		}
		b.WriteString(indent + ft.Name + ": ")
		dumpValue(b, fv, depth+1)
	}
}

// scalar formats a non-zero scalar field for the node's header line.
func scalar(f reflect.StructField, v reflect.Value) (string, bool) {
	if f.Type == posType || v.IsZero() {
		return "", false
	}
	if f.Type == kindType {
		return strconv.Quote(token.Kind(v.Int()).String()), true
	}
	switch v.Kind() {
	case reflect.String:
		return strconv.Quote(v.String()), true
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), true
	case reflect.Int:
		return strconv.FormatInt(v.Int(), 10), true
	}
	return "", false
}
