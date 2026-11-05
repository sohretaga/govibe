// Command vibe is the GoVibe compiler and toolchain.
//
// Usage:
//
//	vibe check [--json]          parse and type-check the project and report diagnostics
//	vibe routes [--json]         list every route, including those crud and auth generate
//	vibe fmt [--check] [--json]  rewrite files in canonical format
//	vibe explain <code> [--json] explain a diagnostic code, such as V201
//
// Every command runs from the project root, the folder that holds the
// project's .vibe files.
package main
