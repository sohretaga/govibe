// Package golden compares test output with golden files under testdata/ and
// rewrites them when tests run with -update.
//
// Every test package imports golden, blank if it has no golden files, so
// that "go test ./... -update" is a defined flag in every test binary.
package golden
