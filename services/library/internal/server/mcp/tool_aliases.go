package mcp

import (
	"context"
	_ "embed"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

type aliasesInput struct {
	Ref string `json:"ref" jsonschema:"Exact metadata ref for the series, e.g. \"tvdb:370070\". Use kura_resolve first if you only have a title."`
}

//go:embed tool_aliases.md
var toolAliasesDoc string

// errEmptyRef is raised by kura_aliases when the ref field is absent.
var errEmptyRef = &invalidInputError{
	kind:    "invalid_ref",
	message: "kura_aliases: ref must not be empty",
}

func addAliasesTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_aliases",
		Title:       "Get all known titles and aliases",
		Description: forLLM(toolAliasesDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintTrue,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in aliasesInput) (*sdkmcp.CallToolResult, any, error) {
		if in.Ref == "" {
			return toolErrorResult(errEmptyRef), nil, nil
		}
		result, err := workflow.MetadataAliases(ctx, deps.Workflow, workflow.MetadataAliasesInput{Ref: in.Ref})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, result, nil
	})
}
