package mcp

import (
	"context"
	_ "embed"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

type resetInput struct {
	Ref     string   `json:"ref" jsonschema:"Metadata ref, e.g. \"tvdb:370070\"."`
	Episode string   `json:"episode,omitempty" jsonschema:"Episode marker (S01E03) or storage form (S01E0003)."`
	Trash   []string `json:"trash,omitempty" jsonschema:"ULIDs of stagedTrash entries to drop."`
	Extras  []string `json:"extras,omitempty" jsonschema:"ULIDs of stagedExtras entries to drop."`
	All     bool     `json:"all,omitempty" jsonschema:"Drop every staged record (episodes + trash + extras) on the series. Mutually exclusive with episode/trash/extras."`
}

type resetOutput struct {
	Cleared      int      `json:"cleared"`
	TrashRemoved []string `json:"trashRemoved,omitempty"`
	ExtraRemoved []string `json:"extraRemoved,omitempty"`
}

//go:embed tool_reset.md
var toolResetDoc string

func addResetTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_reset",
		Title:       "Drop staged media",
		Description: forLLM(toolResetDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in resetInput) (*sdkmcp.CallToolResult, any, error) {
		return runResetTool(ctx, deps, in)
	})
}

// runResetTool is the kura_reset MCP tool handler body. Extracted from
// the AddTool closure so cyclop counts the per-axis input parsing
// against this function rather than addResetTool's setup.
func runResetTool(ctx context.Context, deps Deps, in resetInput) (*sdkmcp.CallToolResult, any, error) {
	metaRef, errResult := validateResetInput(in)
	if errResult != nil {
		return errResult, nil, nil
	}
	seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
	if err != nil {
		return toolErrorResult(err), nil, nil
	}
	if !ok {
		return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
	}
	input, errResult := buildResetWorkflowInput(in, seriesRef)
	if errResult != nil {
		return errResult, nil, nil
	}
	result, err := workflow.Reset(ctx, deps.Workflow, input)
	if err != nil {
		return toolErrorResult(err), nil, nil
	}
	cleared := len(result.Records)
	if result.Record != nil {
		cleared = 1
	}
	return nil, resetOutput{
		Cleared:      cleared,
		TrashRemoved: result.TrashRemoved,
		ExtraRemoved: result.ExtraRemoved,
	}, nil
}

// validateResetInput parses the metadata ref and enforces the
// "at least one mode" + "all is exclusive with episode/trash/extras"
// rules. Returns the parsed ref on success or a populated tool result
// on rejection (tool errors are wire shape, not Go errors).
func validateResetInput(in resetInput) (refs.Metadata, *sdkmcp.CallToolResult) {
	metaRef, err := refs.ParseMetadata(in.Ref)
	if err != nil {
		return "", toolErrorResult(&invalidInputError{
			kind:    errkind.KindInvalidRef,
			message: fmt.Sprintf("kura_reset: %v", err),
		})
	}
	hasEpisode := in.Episode != ""
	hasIDs := len(in.Trash) > 0 || len(in.Extras) > 0
	if !hasEpisode && !hasIDs && !in.All {
		return "", toolErrorResult(&invalidInputError{
			kind:    errkind.KindInvalidRef,
			message: "kura_reset: pass at least one of episode, trash, extras, or all",
		})
	}
	if in.All && (hasEpisode || hasIDs) {
		return "", toolErrorResult(&invalidInputError{
			kind:    errkind.KindInvalidRef,
			message: "kura_reset: all is mutually exclusive with episode/trash/extras",
		})
	}
	return metaRef, nil
}

// buildResetWorkflowInput parses the episode marker + ULID lists into
// a workflow.ResetInput. Returns a populated tool result on parse
// failure (same shape as validateResetInput).
func buildResetWorkflowInput(in resetInput, seriesRef refs.Series) (workflow.ResetInput, *sdkmcp.CallToolResult) {
	input := workflow.ResetInput{Ref: seriesRef, All: in.All}
	if in.Episode != "" {
		episode, err := refs.ParseEpisodeMarker(in.Episode)
		if err != nil {
			return workflow.ResetInput{}, toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_reset: episode: %v", err),
			})
		}
		input.Episode = episode
	}
	for _, raw := range in.Trash {
		id, err := ulid.Parse(raw)
		if err != nil {
			return workflow.ResetInput{}, toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_reset: trash[%s]: %v", raw, err),
			})
		}
		input.TrashIDs = append(input.TrashIDs, id)
	}
	for _, raw := range in.Extras {
		id, err := ulid.Parse(raw)
		if err != nil {
			return workflow.ResetInput{}, toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_reset: extras[%s]: %v", raw, err),
			})
		}
		input.ExtraIDs = append(input.ExtraIDs, id)
	}
	return input, nil
}
