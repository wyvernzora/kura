package mcp

import (
	"context"
	_ "embed"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

type updateTagsInput struct {
	Ref  string   `json:"ref" jsonschema:"Exact metadata ref for the series, e.g. \"tvdb:370070\"."`
	Tags []string `json:"tags" jsonschema:"Tag changes. A plain tag adds it; an expression prefixed with ! removes it. At least one expression is required."`
}

//go:embed tool_update_tags.md
var toolUpdateTagsDoc string

func addUpdateTagsTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_update_tags",
		Title:       "Update series tags",
		Description: forLLM(toolUpdateTagsDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in updateTagsInput) (*sdkmcp.CallToolResult, any, error) {
		metadataRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_update_tags: %v", err),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metadataRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metadataRef}), nil, nil
		}
		result, err := workflow.UpdateTags(ctx, deps.Workflow, workflow.UpdateTagsInput{
			Ref:  seriesRef,
			Tags: in.Tags,
		})
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, result, nil
	})
}
