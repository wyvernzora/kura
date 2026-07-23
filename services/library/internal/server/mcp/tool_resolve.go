package mcp

import (
	"context"
	_ "embed"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// resolveInput is the kura_resolve tool's typed input. Schema is
// auto-generated from the struct + jsonschema tag descriptions by
// mcp.AddTool. Length bounds aren't expressible via tag (jsonschema-go
// only infers MinItems/MaxItems from fixed-size arrays); the handler
// validates non-emptiness explicitly.
type resolveInput struct {
	Terms []string `json:"terms" jsonschema:"Selector terms. Each is either a free-text title fragment matched against series titles in any source language (e.g. \"bookworm\", \"honzuki\") or one explicit metadata ref (e.g. \"tvdb:370070\"). Must not be empty."`
}

//go:embed tool_resolve.md
var toolResolveDoc string

func addResolveTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_resolve",
		Title:       "Resolve metadata candidates",
		Description: forLLM(toolResolveDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintTrue,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in resolveInput) (*sdkmcp.CallToolResult, any, error) {
		if len(in.Terms) == 0 {
			return toolErrorResult(errEmptyTerms), nil, nil
		}
		result, err := workflow.Resolve(ctx, deps.Workflow, workflow.ResolveInput{Terms: in.Terms})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, result, nil
	})
}
