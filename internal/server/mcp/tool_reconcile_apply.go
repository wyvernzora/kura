package mcp

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"regexp"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

type reconcileApplyInput struct {
	Ref   string `json:"ref" jsonschema:"Metadata ref, e.g. \"tvdb:370070\"."`
	Token string `json:"token" jsonschema:"Plan token from kura_reconcile_plan (12-char hex)."`
}

var planTokenPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)

//go:embed tool_reconcile_apply.md
var toolReconcileApplyDoc string

func addReconcileApplyTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_reconcile_apply",
		Title:       "Apply reconcile plan",
		Description: forLLM(toolReconcileApplyDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintTrue,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in reconcileApplyInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_reconcile_apply: %v", err),
			}), nil, nil
		}
		if !planTokenPattern.MatchString(in.Token) {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_reconcile_apply: invalid token %q; expected 12-char hex", in.Token),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		// Pre-submission peek per design/async-job.md § 8: surface a
		// cross-process claim as a synchronous BusyError before
		// spawning a goroutine that would race the peer's CAS.
		// Racy by design — a peer can claim between peek and submit.
		// If they do, the goroutine returns BusyError as a terminal
		// failure on the job. Both paths produce the same kind="busy"
		// in the agent's view; the peek just speeds up the common case.
		if err := peekReconcileBusy(deps.Workflow.LibRoot, seriesRef); err != nil {
			return toolErrorResult(err), nil, nil
		}
		j := workflow.ApplyReconcile(ctx, deps.Workflow, workflow.ApplyReconcileInput{Ref: seriesRef, Token: in.Token})
		// Three-branch IsTracked handler per design/async-job.md § 11.b.
		if !j.IsTracked() {
			_, waitErr := j.Wait(ctx)
			if waitErr != nil {
				return toolErrorResult(waitErr), nil, nil
			}
			return toolErrorResult(fmt.Errorf("internal: kura_reconcile_apply returned untracked success (workflow bug)")), nil, nil
		}
		return nil, jobHandleOutput{JobID: j.ID()}, nil
	})
}

// peekReconcileBusy reads the target series.json once and returns a
// coord.BusyError when an in_progress claim is set. Missing or
// unreadable series.json is not an error — the workflow itself will
// surface anything load-related when it runs.
func peekReconcileBusy(libRoot string, ref refs.Series) error {
	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return nil
	}
	if model.InProgress == nil {
		return nil
	}
	return &coord.BusyError{
		Scope:  coord.SeriesScope(ref),
		Holder: *model.InProgress,
	}
}
