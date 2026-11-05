// Package vibe is the runtime of every app that GoVibe builds. vibe build
// copies its source into the app's build directory as the package
// vibeapp/runtime, and the generated code imports it under the name vibe.
//
// It holds what generated code shares: the HTTP server and its JSON
// responses, status names and errors, request input decoding and
// validation, Money, UUIDv7 IDs, slugs, the SQLite connection pools and
// schema sync. It uses only the standard library: the generated main
// package imports the SQLite driver, so the compiler module that holds
// this source never depends on it.
package vibe
