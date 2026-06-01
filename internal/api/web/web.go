// Package web bundles the small set of static HTML assets shipped with the
// API binary. Embedding them keeps the binary self-contained — operators
// don't have to mount a `dist/` directory or run a frontend build to get
// the workers dashboard or the public status page.
package web

import (
	"embed"
	"io/fs"
)

//go:embed workers.html
var WorkersHTML []byte

//go:embed statuspage.html
var StatusPageHTML string

//go:embed console/dist/*
var consoleDist embed.FS

func ConsoleDist() (fs.FS, error) {
	return fs.Sub(consoleDist, "console/dist")
}
