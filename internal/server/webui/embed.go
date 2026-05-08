// Package webui embeds the production web UI bundle and serves it as
// a SPA from the same kura HTTP server that handles /api/v1/* and the
// MCP transport.
//
// During development the dist/ subtree only holds a placeholder
// index.html. `make web-build` populates dist/ with the Vite output
// (index.html plus content-hashed asset files), at which point the
// embed picks up the real bundle on the next `go build`.
package webui

import "embed"

//go:embed all:dist
var distFS embed.FS
