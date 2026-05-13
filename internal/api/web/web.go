// Package web bundles the small set of static HTML assets shipped with the
// API binary. Embedding them keeps the binary self-contained — operators
// don't have to mount a `dist/` directory or run a frontend build to get
// the workers dashboard.
package web

import _ "embed"

//go:embed workers.html
var WorkersHTML []byte
