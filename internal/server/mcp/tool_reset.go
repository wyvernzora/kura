package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

type resetInput struct {
	Ref     string `json:"ref" jsonschema:"Metadata ref (e.g. \"tvdb:370070\") from kura_resolve."`
	Episode string `json:"episode,omitempty" jsonschema:"Episode marker (S01E03) or storage form (S01E0003). Mutually exclusive with all."`
	All     bool   `json:"all,omitempty" jsonschema:"Drop every staged record on the series. Mutually exclusive with episode."`
}

type resetOutput struct {
	Cleared int `json:"cleared"`
}

const resetDescription = `Drop staged media records on a series — the intent records that ` + "`kura_stage`" + ` created and ` + "`kura_reconcile_apply`" + ` would otherwise act on. No files are touched; only the in-library intent is cleared.

Pass ` + "`episode`" + ` to drop one slot's staged record, or ` + "`all`" + ` to drop every staged record on the series. Exactly one is required.

Active media records are not affected — use ` + "`kura_stage`" + ` with ` + "`replace=true`" + ` followed by reconcile if you want to displace an active record.`

func addResetTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_reset",
		Title:       "Drop staged media",
		Description: resetDescription,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in resetInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_reset: %v", err),
			}), nil, nil
		}
		hasEpisode := in.Episode != ""
		if hasEpisode == in.All {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: "kura_reset: pass exactly one of episode or all",
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		input := workflow.ResetInput{Ref: seriesRef, All: in.All}
		if hasEpisode {
			episode, err := refs.ParseEpisodeMarker(in.Episode)
			if err != nil {
				return toolErrorResult(&invalidInputError{
					kind:    errkind.KindInvalidRef,
					message: fmt.Sprintf("kura_reset: episode: %v", err),
				}), nil, nil
			}
			input.Episode = episode
		}
		result, err := workflow.Reset(ctx, deps.Workflow, input)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		cleared := len(result.Records)
		if result.Record != nil {
			cleared = 1
		}
		return nil, resetOutput{Cleared: cleared}, nil
	})
}
