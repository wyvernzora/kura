// Package mcp hosts the kura MCP transport. Tools are added in
// follow-up commits; this package currently exposes a server skeleton
// plus stdio and streamable-HTTP transport runners.
package mcp

import (
	"context"
	"errors"
	"net/http"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/workflow"
)

const (
	serverName    = "kura"
	serverVersion = "0.1.0"
)

// httpShutdownGrace caps how long ServeHTTP waits for in-flight
// requests after the parent ctx is cancelled. Independent from the
// jobs registry shutdown grace; this only covers the HTTP listener.
const httpShutdownGrace = 5 * time.Second

// hintTrue / hintFalse are addressable bool literals shared by tool
// registrations; ToolAnnotations takes *bool for fields whose default
// matters and these saves a per-tool helper.
var (
	hintTrue  = true
	hintFalse = false
)

// Deps bundles everything the MCP transport needs to construct its
// tool handlers. Tools land in later commits and consume Workflow.
type Deps struct {
	Workflow workflow.Deps
}

// NewServer constructs the MCP server with kura's capabilities and
// registers the tool surface. Each tool lives in its own file
// (tool_*.go) and exposes an addXxxTool helper that wires its handler
// into the server.
func NewServer(deps Deps) *sdkmcp.Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)
	addResolveTool(s, deps)
	addListTool(s, deps)
	addShowTool(s, deps)
	addAddTool(s, deps)
	addStageTool(s, deps)
	addResetTool(s, deps)
	addReconcilePlanTool(s, deps)
	return s
}

// ServeStdio runs the MCP server over stdin/stdout. Returns when the
// peer closes the transport, ctx is cancelled, or an unrecoverable
// transport error occurs.
func ServeStdio(ctx context.Context, server *sdkmcp.Server) error {
	return server.Run(ctx, &sdkmcp.StdioTransport{})
}

// ServeHTTP runs the MCP server over the streamable-HTTP transport on
// the given address. The same *mcp.Server is returned for every HTTP
// request; the SDK creates per-request sessions against it.
//
// On ctx cancellation, the underlying http.Server is given
// httpShutdownGrace to drain in-flight requests before forced close.
func ServeHTTP(ctx context.Context, addr string, server *sdkmcp.Server) error {
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, nil)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			errCh <- nil
			return
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), httpShutdownGrace)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}
