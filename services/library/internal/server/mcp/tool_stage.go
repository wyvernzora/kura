package mcp

import (
	"context"
	_ "embed"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

type stageInput struct {
	Ref      string                  `json:"ref" jsonschema:"Metadata ref, e.g. \"tvdb:370070\"."`
	Episodes []stageEpisodeInputItem `json:"episodes,omitempty" jsonschema:"Episode stages. One per spine slot. At least one of episodes/trash/extras is required."`
	Trash    []stageTrashInputItem   `json:"trash,omitempty" jsonschema:"Files queued for trash on the next reconcile_apply."`
	Extras   []stageExtraInputItem   `json:"extras,omitempty" jsonschema:"Files or directories queued for placement under Season N/Extra/[prefix]/ on next reconcile_apply."`
}

type stageEpisodeInputItem struct {
	Episode    string   `json:"episode" jsonschema:"Episode marker (S01E03) or storage form (S01E0003)."`
	Media      string   `json:"media" jsonschema:"File to stage. Accepts inbox: or series: selectors. Series: selectors must be unclaimed, except for an in-place metadata override of this episode's active file."`
	Source     string   `json:"source,omitempty" jsonschema:"Override for the source label (BluRay, WebRip, Web-DL, HDTV, DVDRip, TVRip, Unknown)."`
	Companions []string `json:"companions,omitempty" jsonschema:"Inbox sidecar selectors. Current MCP parsing accepts companions only for inbox media."`
	Replace    bool     `json:"replace,omitempty" jsonschema:"Allow staging over an existing active or staged record at this slot."`
}

type stageTrashInputItem struct {
	Path       string   `json:"path" jsonschema:"Series selector for the file to queue (e.g. 'series:Season 1/foo.mkv'). Scoped to the series in the request's ref."`
	Companions []string `json:"companions,omitempty" jsonschema:"Sidecar series: selectors to drag along into trash."`
}

type stageExtraInputItem struct {
	Season int    `json:"season" jsonschema:"Season number under which the extra is placed."`
	Source string `json:"source" jsonschema:"Inbox selector pointing at a file or directory under the inbox root, e.g. 'inbox:[BDrip] Show/Extras/'."`
	Prefix string `json:"prefix,omitempty" jsonschema:"Optional sub-folder under Season N/Extra/."`
}

//go:embed tool_stage.md
var toolStageDoc string

// maxStageBatchSize caps each kura_stage input array. Realistic batches
// (full-cour BD ≈ 13 episodes; full season ≤ 26) sit well under this;
// the cap defends availability against a misbehaving agent that would
// otherwise hold the per-series mutex for thousands of mediainfo probes
// in one call.
const maxStageBatchSize = 100

func overCap(field string, n int) error {
	if n <= maxStageBatchSize {
		return nil
	}
	return &invalidInputError{
		kind:    errkind.KindBatchTooLarge,
		message: fmt.Sprintf("kura_stage: %s length %d exceeds cap of %d", field, n, maxStageBatchSize),
	}
}

func addStageTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_stage",
		Title:       "Stage media batch for series",
		Description: forLLM(toolStageDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  false,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in stageInput) (*sdkmcp.CallToolResult, any, error) {
		return runStageTool(ctx, deps, in)
	})
}

// runStageTool is the kura_stage MCP tool handler body. Extracted from
// the AddTool closure so cyclop scores the per-axis input parsing
// against this function rather than addStageTool's setup.
func runStageTool(ctx context.Context, deps Deps, in stageInput) (*sdkmcp.CallToolResult, any, error) {
	metaRef, err := refs.ParseMetadata(in.Ref)
	if err != nil {
		return toolErrorResult(&invalidInputError{
			kind:    errkind.KindInvalidRef,
			message: fmt.Sprintf("kura_stage: %v", err),
		}), nil, nil
	}
	if over := overCap("episodes", len(in.Episodes)); over != nil {
		return toolErrorResult(over), nil, nil
	}
	if over := overCap("trash", len(in.Trash)); over != nil {
		return toolErrorResult(over), nil, nil
	}
	if over := overCap("extras", len(in.Extras)); over != nil {
		return toolErrorResult(over), nil, nil
	}
	stageIn, err := workflow.BuildStageInput(stageInputToWorkflow(in))
	if err != nil {
		return toolErrorResult(&invalidInputError{
			kind:    errkind.KindInvalidRef,
			message: fmt.Sprintf("kura_stage: %v", err),
		}), nil, nil
	}
	seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
	if err != nil {
		return toolErrorResult(err), nil, nil
	}
	if !ok {
		return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
	}
	stageIn.Ref = seriesRef

	j := workflow.Stage(ctx, deps.Workflow, stageIn)
	// Three-branch IsTracked handler per design/async-job.md § 11.b.
	if !j.IsTracked() {
		_, waitErr := j.Wait(ctx)
		if waitErr != nil {
			return toolErrorResult(waitErr), nil, nil
		}
		return toolErrorResult(fmt.Errorf("internal: kura_stage returned untracked success (workflow bug)")), nil, nil
	}
	return nil, jobHandleOutput{JobID: j.ID()}, nil
}

// stageInputToWorkflow flattens the MCP wire-shape stageInput into the
// transport-neutral workflow.StageRequest. One field-by-field copy
// per axis; the workflow side owns selector + episode-marker parsing.
func stageInputToWorkflow(in stageInput) workflow.StageRequest {
	out := workflow.StageRequest{
		Episodes: make([]workflow.StageRequestEpisode, 0, len(in.Episodes)),
		Trash:    make([]workflow.StageRequestTrash, 0, len(in.Trash)),
		Extras:   make([]workflow.StageRequestExtra, 0, len(in.Extras)),
	}
	for _, ep := range in.Episodes {
		out.Episodes = append(out.Episodes, workflow.StageRequestEpisode{
			Episode:    ep.Episode,
			Media:      ep.Media,
			Source:     ep.Source,
			Companions: ep.Companions,
			Replace:    ep.Replace,
		})
	}
	for _, t := range in.Trash {
		out.Trash = append(out.Trash, workflow.StageRequestTrash{
			Path:       t.Path,
			Companions: t.Companions,
		})
	}
	for _, ex := range in.Extras {
		out.Extras = append(out.Extras, workflow.StageRequestExtra{
			Season: ex.Season,
			Source: ex.Source,
			Prefix: ex.Prefix,
		})
	}
	return out
}
