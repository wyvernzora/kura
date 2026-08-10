// Package mcp is the suite MCP surface: one server, one tool catalog, over
// both leaf services.
//
// The catalog is static for the lifetime of the process. A leaf being down
// changes tool *results*, never tools/list — an agent that cannot see a tool
// concludes the capability does not exist, which is a worse failure than a
// call that returns backend_unavailable.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/gateway/internal/client"
	"github.com/wyvernzora/kura/services/gateway/internal/metrics"
)

const (
	// serverName identifies the suite in the initialize handshake. One
	// product, one name — the leaves are an implementation detail.
	serverName = "kura"

	// Endpoint is the streamable-HTTP path. Its version is the gateway's
	// own axis: the leaf REST surfaces version independently under
	// /api/<service>/v1, but the MCP surface is gateway-composed and
	// bumps on its own schedule.
	Endpoint = "/mcp/v1"

	// responseBudget bounds the two projections that can carry an
	// unbounded payload.
	responseBudget = 80 << 10
)

// Server holds the leaf clients and the built SDK server.
type Server struct {
	library  *client.Client
	releases *client.Client
	logger   *slog.Logger
	sdk      *mcpsdk.Server

	// Metrics, when non-nil, records per-tool call counts and durations.
	// Set once at wiring, before the handler serves.
	Metrics *metrics.Metrics
}

// New builds the server with all 16 tools registered.
func New(version string, library, releases *client.Client, logger *slog.Logger) *Server {
	s := &Server{library: library, releases: releases, logger: logger}
	sdk := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: version}, &mcpsdk.ServerOptions{
		Instructions: instructions,
	})
	registerLibraryTools(sdk, s)
	registerReleaseTools(sdk, s)
	s.sdk = sdk
	return s
}

// Handler serves the MCP endpoint and expires idle transport sessions.
func (s *Server) Handler(sessionTimeout time.Duration) http.Handler {
	return mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return s.sdk },
		&mcpsdk.StreamableHTTPOptions{SessionTimeout: sessionTimeout},
	)
}

func (s *Server) log(ctx context.Context, level slog.Level, msg string, attrs ...any) {
	if s.logger == nil {
		return
	}
	s.logger.Log(ctx, level, msg, attrs...)
}

// observe emits the one structured line per tool call that is the whole
// observability story for the bridge: tool, upstream status, duration, and
// error kind. Bodies and magnet URIs are deliberately absent.
func (s *Server) observe(ctx context.Context, tool string, start time.Time, err error, extra ...any) {
	elapsed := time.Since(start)
	s.Metrics.ToolCall(tool, elapsed, err)
	attrs := append([]any{
		"tool", tool,
		"duration_ms", elapsed.Milliseconds(),
	}, extra...)
	if err != nil {
		var env *client.Error
		kind, status := client.KindInternal, 0
		if asClientError(err, &env) {
			kind, status = env.Kind, env.Status
		}
		attrs = append(attrs, "kind", kind, "upstream_status", status)
		s.log(ctx, slog.LevelWarn, "mcp tool failed", attrs...)
		return
	}
	s.log(ctx, slog.LevelInfo, "mcp tool completed", attrs...)
}

// errorResult renders a failure as an MCP tool result. Application errors are
// tool results, never MCP protocol errors — the protocol level is reserved for
// malformed MCP traffic. Error results carry no structuredContent.
func errorResult(err error) *mcpsdk.CallToolResult {
	var env *client.Error
	if !asClientError(err, &env) {
		env = &client.Error{Kind: client.KindInternal, Message: err.Error()}
	}
	body, _ := json.Marshal(client.Error{Kind: env.Kind, Message: env.Message, Data: env.Data})
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(body)}},
	}
}

func readOnly() *mcpsdk.ToolAnnotations {
	no, yes := false, true
	return &mcpsdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &yes}
}

func readOnlyClosed() *mcpsdk.ToolAnnotations {
	no := false
	return &mcpsdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no}
}

// mutating describes a write. idempotent and destructive follow the plan's
// per-tool annotation columns; open reports whether the call can reach an
// external provider.
func mutating(idempotent, destructive, open bool) *mcpsdk.ToolAnnotations {
	return &mcpsdk.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: &destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &open,
	}
}

type toolHandler[In, Out any] func(context.Context, *mcpsdk.CallToolRequest, In) (*mcpsdk.CallToolResult, Out, error)

// addTool registers a typed tool with a resolved output schema.
//
// List caps are deliberately NOT expressed as JSON-Schema maxima: the SDK
// validates the schema before the handler runs, so a declared maximum turns a
// legal-but-large request into a schema rejection instead of the documented
// clamp. The caps live in the handlers and are stated in each description.
func addTool[In, Out any](srv *mcpsdk.Server, tool *mcpsdk.Tool, h toolHandler[In, Out]) {
	outputSchema, err := jsonschema.For[Out](nil)
	if err != nil {
		panic(fmt.Sprintf("addTool %q: output schema: %v", tool.Name, err))
	}
	if _, err := outputSchema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true}); err != nil {
		panic(fmt.Sprintf("addTool %q: resolve output schema: %v", tool.Name, err))
	}
	tt := *tool
	tt.OutputSchema = outputSchema
	mcpsdk.AddTool[In, any](srv, &tt, func(ctx context.Context, req *mcpsdk.CallToolRequest, input In) (*mcpsdk.CallToolResult, any, error) {
		res, out, err := h(ctx, req, input)
		if err != nil {
			return res, nil, err
		}
		if res != nil && res.IsError {
			return res, nil, nil
		}
		return res, out, nil
	})
}

func asClientError(err error, target **client.Error) bool {
	for e := err; e != nil; {
		if ce, ok := e.(*client.Error); ok {
			*target = ce
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}
