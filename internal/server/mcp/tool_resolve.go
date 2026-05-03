package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/workflow"
)

// resolveInput is the kura_resolve tool's typed input. Schema is
// auto-generated from the struct + jsonschema tag descriptions by
// mcp.AddTool. Length bounds aren't expressible via tag (jsonschema-go
// only infers MinItems/MaxItems from fixed-size arrays); the handler
// validates non-emptiness explicitly.
type resolveInput struct {
	Terms []string `json:"terms" jsonschema:"Selector terms. Each is either a free-text title fragment matched against series titles in any source language (e.g. \"bookworm\", \"honzuki\") or one explicit metadata ref (e.g. \"tvdb:370070\"). Must not be empty."`
}

const resolveDescription = `Look up metadata candidates for a series. Accepts free-text title fragments or one explicit metadata ref (e.g. ` + "`tvdb:370070`" + `).

Returns a ` + "`candidates`" + ` array. Cardinality:
- 0: no match.
- 1: unique.
- 2+: ambiguous.

Each candidate carries ` + "`evidence`" + ` (which term matched, where, with qualifying annotations like ` + "`full_match`" + `) for ranking heuristics. Empty for explicit-ref lookups.`

func addResolveTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_resolve",
		Title:       "Resolve metadata candidates",
		Description: resolveDescription,
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
