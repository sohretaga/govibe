package vibe

import "embed"

// Source holds the source of this package. vibe build copies every file
// except tests and this one into the app it builds.
//
//go:embed *.go
var Source embed.FS
