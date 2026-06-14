package mcp

import (
	"context"
	_ "embed"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/provider/tvdb"
	"github.com/wyvernzora/kura/internal/workflow"
)

type addInput struct {
	Ref      string `json:"ref" jsonschema:"Metadata ref, e.g. \"tvdb:370070\"."`
	Dirname  string `json:"dirname,omitempty" jsonschema:"Optional override for the on-disk directory name. Defaults to a sanitized form of the provider's preferred title."`
	Ordering string `json:"ordering,omitempty" jsonschema:"Pin the per-series episode ordering used for the initial spine fetch. One of: default, official, dvd, absolute, alternate, regional. Omit to use the provider's default."`
}

//go:embed tool_add.md
var toolAddDoc string

func addAddTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_add",
		Title:       "Add series to library",
		Description: forLLM(toolAddDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  false,
			OpenWorldHint:   &hintTrue,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in addInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_add: %v", err),
			}), nil, nil
		}
		var seriesRef refs.Series
		if in.Dirname != "" {
			seriesRef, err = refs.ParseSeries(in.Dirname)
			if err != nil {
				return toolErrorResult(&invalidInputError{
					kind:    errkind.KindInvalidRef,
					message: fmt.Sprintf("kura_add: dirname: %v", err),
				}), nil, nil
			}
		}
		ordering, err := tvdb.ParseOrdering(in.Ordering)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_add: %v", err),
			}), nil, nil
		}
		if _, err := workflow.Add(ctx, deps.Workflow, workflow.AddInput{Metadata: metaRef, Ref: seriesRef, Ordering: ordering}); err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, ackSuccess, nil
	})
}
