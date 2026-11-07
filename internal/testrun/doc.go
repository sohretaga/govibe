// Package testrun runs the tests of a program for vibe test. It writes
// the app with its generated tests to .vibe/build, runs them with
// go test -json and reports each test by its GoVibe name, with the .vibe
// line, message and values of a failure.
package testrun
