// Command vibe is the GoVibe compiler and toolchain.
//
// Usage:
//
//	vibe check [--json]          lex and parse the project and report diagnostics
//	vibe fmt [--check] [--json]  rewrite files in canonical format
//
// Every command runs from the project root, the folder that holds the
// project's .vibe files.
package main
