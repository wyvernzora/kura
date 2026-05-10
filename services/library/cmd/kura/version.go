package main

// Version is the kura build version. Stamped at link time via
// `-ldflags="-X main.Version=<value>"`. Defaults to "dev" so ad-hoc
// `go build` and `go run` invocations still produce a usable binary.
//
// Surfaces:
//   - boot log "kura serve starting"
//   - MCP server.Implementation.Version (internal/server/mcp)
//   - REST /api/v1/health response and X-Kura-Version header
var Version = "dev"
