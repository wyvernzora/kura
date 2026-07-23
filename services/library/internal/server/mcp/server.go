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

	"github.com/wyvernzora/kura/services/library/internal/server/auth"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

const (
	serverName = "kura"

	// defaultServerVersion is the fallback advertised when Deps.Version
	// is empty. Production callers (cmd/kura) inject the build-time
	// main.Version; this default keeps tests and direct library use
	// from blowing up on an empty Implementation.Version.
	defaultServerVersion = "dev"
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
//
// BearerToken, when non-empty, gates the streamable-HTTP transport
// (not stdio): every HTTP request must carry "Authorization: Bearer
// <token>" or be rejected with 401. Stdio is unauthenticated by
// design — process boundary already trusts the parent.
type Deps struct {
	Workflow    workflow.Deps
	Logger      *slog.Logger
	BearerToken string

	// Version surfaces in the MCP Implementation handshake. Empty
	// falls back to defaultServerVersion. cmd/kura sets this to the
	// build-time main.Version so the value flows from a single source.
	Version string
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
	version := deps.Version
	if version == "" {
		version = defaultServerVersion
	}
	s := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: version,
	}, &sdkmcp.ServerOptions{
		Instructions: forLLM(serverInstructions),
	})
	if deps.Logger != nil {
		s.AddReceivingMiddleware(toolCallLoggingMiddleware(deps.Logger))
	}
	addAliasesTool(s, deps)
	addResolveTool(s, deps)
	addListTool(s, deps)
	addShowTool(s, deps)
	addUpdateTagsTool(s, deps)
	addAddTool(s, deps)
	addImportTool(s, deps)
	addStageTool(s, deps)
	addResetTool(s, deps)
	addReconcilePlanTool(s, deps)
	addScanTool(s, deps)
	addReconcileApplyTool(s, deps)
	addJobStatusTool(s, deps)
	addInboxListTool(s, deps)
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
				argsRaw  json.RawMessage
			)
			if params, ok := req.GetParams().(*sdkmcp.CallToolParamsRaw); ok && params != nil {
				toolName = params.Name
				argsRaw = params.Arguments
			}
			result, err := next(ctx, method, req)
			// argsRaw is json.RawMessage ([]byte). slog renders byte
			// slices as decimal arrays unless cast to string; cast
			// here so the log line is readable JSON text.
			attrs := []any{
				"tool", toolName,
				"args", string(argsRaw),
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
//
// bearerToken, when non-empty, gates access via "Authorization:
// Bearer <token>". MCP-stdio is unauthenticated (process boundary);
// only the HTTP transport reaches this code path.
func ServeHTTP(ctx context.Context, addr string, server *sdkmcp.Server, bearerToken string) error {
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return server
	}, nil)
	gated := auth.BearerMiddleware(bearerToken)(handler)
	// Mount under /mcp per MCP Streamable HTTP convention so the path
	// is interoperable with the broader ecosystem (inspector defaults,
	// reverse-proxy layouts that route /mcp to the transport while
	// reserving / for REST or a webui).
	mux := http.NewServeMux()
	mux.Handle("/mcp", gated)
	mux.Handle("/mcp/", gated)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
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
		// Fresh ctx is intentional — the inherited one is already
		// cancelled (parent triggered shutdown), so reusing it
		// would skip the drain entirely. Standard graceful-
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
