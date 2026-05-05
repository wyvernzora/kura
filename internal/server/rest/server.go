// Package rest hosts kura's REST transport. The package mirrors
// internal/server/mcp in shape: NewServer wires handlers against a
// workflow.Deps; Serve binds the HTTP listener and shuts it down on
// context cancellation.
//
// REST is the migration target for the CLI (per scratch/design/rest.md):
// every workflow function gets a JSON endpoint, and cmd/kura verbs
// become thin clients of the same endpoints WebUI eventually consumes.
// Single-writer architecture per scratch/Product.md "Single writer at
// any moment" — REST + MCP share one Coordinator inside one process.
package rest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/workflow"
)

const (
	serverVersion = "0.1.0"

	// httpShutdownGrace caps how long Serve waits for in-flight requests
	// after the parent ctx is cancelled. Mirrors mcp.httpShutdownGrace.
	httpShutdownGrace = 5 * time.Second

	// readHeaderTimeout caps how long the server waits for request
	// headers before timing out.
	readHeaderTimeout = 10 * time.Second
)

// Deps bundles everything the REST transport needs.
//
//   - Workflow: shared workflow dependency set (same one MCP uses).
//   - Logger: optional structured logger; nil disables per-request logs.
//   - AllowedOrigins: CORS allow-list. Empty = deny all cross-origin
//     requests; "*" = allow any origin. Same-origin requests (no Origin
//     header) are unaffected.
//   - BearerToken: when non-empty, every request must carry
//     "Authorization: Bearer <token>" or be rejected with 401. Empty
//     means auth disabled — only set this when the deployer explicitly
//     opts out via KURA_DISABLE_TOKEN.
type Deps struct {
	Workflow       workflow.Deps
	Logger         *slog.Logger
	AllowedOrigins []string
	BearerToken    string
}

// Server holds the prebuilt http.Handler plus startup-time metadata
// (process start instant for /health uptime). One instance per
// `kura serve --rest` invocation; safe for concurrent requests.
type Server struct {
	deps      Deps
	handler   http.Handler
	startedAt time.Time
}

// NewServer constructs the REST server with kura's endpoint surface.
// Each handler lives in its own file (handler_*.go) and binds itself
// in router.go.
func NewServer(deps Deps) *Server {
	s := &Server{deps: deps, startedAt: time.Now()}
	s.handler = s.buildRouter()
	return s
}

// Handler exposes the http.Handler for tests + embedding. Production
// callers go through Serve, which adds bind safety + graceful shutdown.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ServeOptions captures bind-time configuration. Pure data so callers
// can extend without breaking Serve's signature.
type ServeOptions struct {
	// PortFile, when non-empty, receives the resolved TCP port after
	// binding (atomic write via temp + rename). Lets test harnesses
	// discover the port when addr ends in ":0".
	PortFile string
}

// Serve binds the REST surface on addr. Bind-safety is now enforced
// by the bearer-token gate (set via Deps.BearerToken at NewServer
// time): a public bind without a valid token returns 401 to all
// callers. On ctx cancellation the listener gets httpShutdownGrace
// to drain in-flight requests before forced close.
func Serve(ctx context.Context, addr string, opts ServeOptions, server *Server) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("rest listen %s: %w", addr, err)
	}
	if opts.PortFile != "" {
		port := listener.Addr().(*net.TCPAddr).Port
		if err := writePortFile(opts.PortFile, port); err != nil {
			_ = listener.Close()
			return err
		}
	}
	httpServer := &http.Server{
		Handler:           server.handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	errCh := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			errCh <- nil
			return
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		// Intentionally fresh ctx: the inherited one is already
		// cancelled (that's why we're shutting down), so passing
		// it to Shutdown would skip the drain. Standard graceful-
		// shutdown pattern. contextcheck linter is excluded for
		// this file via .golangci.yml.
		shutCtx, cancel := context.WithTimeout(context.Background(), httpShutdownGrace)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// writePortFile atomically writes port as an ASCII decimal followed by
// a newline. Atomic via temp file + rename so polling readers never
// see a partial write.
func writePortFile(path string, port int) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, fmt.Appendf(nil, "%d\n", port), 0o644); err != nil {
		return fmt.Errorf("write port file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename port file: %w", err)
	}
	return nil
}
