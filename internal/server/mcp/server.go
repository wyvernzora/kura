// Package mcp hosts the kura MCP transport. Tools are added in
// follow-up commits; this package currently exposes a server skeleton
// plus stdio and streamable-HTTP transport runners.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
// tool handlers. Tools consume Workflow; Logger is optional (nil →
// no per-call logging).
type Deps struct {
	Workflow workflow.Deps
	Logger   *slog.Logger
}

// NewServer constructs the MCP server with kura's capabilities and
// registers the tool surface. Each tool lives in its own file
// (tool_*.go) and exposes an addXxxTool helper that wires its handler
// into the server.
//
// When Deps.Logger is non-nil, an inbound middleware logs every
// `tools/call` with the tool name + raw arguments + duration + error
// status. Other JSON-RPC methods (initialize, ping, etc.) are not
// logged — only tool calls carry actionable application semantics.
func NewServer(deps Deps) *sdkmcp.Server {
	s := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)
	if deps.Logger != nil {
		s.AddReceivingMiddleware(toolCallLoggingMiddleware(deps.Logger))
	}
	addResolveTool(s, deps)
	addListTool(s, deps)
	addShowTool(s, deps)
	addAddTool(s, deps)
	addImportTool(s, deps)
	addStageTool(s, deps)
	addResetTool(s, deps)
	addReconcilePlanTool(s, deps)
	addScanTool(s, deps)
	addReconcileApplyTool(s, deps)
	addJobStatusTool(s, deps)
	addTrashTool(s, deps)
	return s
}

// toolCallLoggingMiddleware emits one structured log line per
// inbound `tools/call`. Captures tool name, raw JSON arguments,
// duration, and error / IsError outcome. Skips other methods so
// transport-level chatter (initialize, ping, list_tools) doesn't
// flood the log.
func toolCallLoggingMiddleware(logger *slog.Logger) sdkmcp.Middleware {
	return func(next sdkmcp.MethodHandler) sdkmcp.MethodHandler {
		return func(ctx context.Context, method string, req sdkmcp.Request) (sdkmcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			started := time.Now()
			var (
				toolName string
				args     json.RawMessage
			)
			if params, ok := req.GetParams().(*sdkmcp.CallToolParamsRaw); ok && params != nil {
				toolName = params.Name
				args = params.Arguments
			}
			result, err := next(ctx, method, req)
			attrs := []any{
				"tool", toolName,
				"args", args,
				"duration_ms", time.Since(started).Milliseconds(),
			}
			if err != nil {
				logger.Error("mcp tool call failed", append(attrs, "err", err)...)
				return result, err
			}
			if cr, ok := result.(*sdkmcp.CallToolResult); ok && cr != nil && cr.IsError {
				logger.Warn("mcp tool call returned error", attrs...)
				return result, nil
			}
			logger.Info("mcp tool call", attrs...)
			return result, nil
		}
	}
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
