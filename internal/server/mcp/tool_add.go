package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

type addInput struct {
	Ref     string `json:"ref" jsonschema:"Metadata ref (e.g. \"tvdb:370070\") from kura_resolve."`
	Dirname string `json:"dirname,omitempty" jsonschema:"Optional override for the on-disk directory name. Defaults to a sanitized form of the provider's preferred title."`
}

const addDescription = `Register a new series in the library at the given metadata ref. Creates a directory under the library root for the series so subsequent tools (kura_show, kura_stage, kura_scan, etc.) can act on it.

` + "`dirname`" + ` overrides the directory name; defaults to a sanitized form of the provider's preferred title.

Refuses if the ref is already tracked or the target directory already exists.`

func addAddTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_add",
		Title:       "Add series to library",
		Description: addDescription,
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
		if _, err := workflow.Add(ctx, deps.Workflow, workflow.AddInput{Metadata: metaRef, Ref: seriesRef}); err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, nil, nil
	})
}
