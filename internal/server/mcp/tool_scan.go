package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/workflow"
)

type scanInput struct {
	Ref string `json:"ref" jsonschema:"Metadata ref (e.g. \"tvdb:370070\") from kura_resolve."`
}

type jobHandleOutput struct {
	JobID string `json:"jobId"`
}

const scanDescription = `Walk the series's directory, parse each media file, refresh provider metadata, and update the per-episode active records. Returns a ` + "`jobId`" + ` immediately; poll ` + "`kura_job_status`" + ` for progress and final result.

Scan can take seconds to minutes (NFS, mediainfo probing) so it runs as a tracked job. The agent can do other work while it finishes.

Files inferred to a slot the provider's spine doesn't have are soft-skipped (kind ` + "`metadata_slot_missing`" + `) rather than aborting the whole scan.

Re-submitting on a series with an in-flight scan returns the same ` + "`jobId`" + ` (deduped at the registry).`

func addScanTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_scan",
		Title:       "Scan series for media files",
		Description: scanDescription,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    false,
			IdempotentHint:  false,
			OpenWorldHint:   &hintTrue,
			DestructiveHint: &hintFalse,
		},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in scanInput) (*sdkmcp.CallToolResult, any, error) {
		metaRef, err := refs.ParseMetadata(in.Ref)
		if err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_scan: %v", err),
			}), nil, nil
		}
		seriesRef, ok, err := deps.Workflow.Index.Get(metaRef)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		if !ok {
			return toolErrorResult(&workflow.MetadataRefNotIndexedError{Ref: metaRef}), nil, nil
		}
		j := workflow.Scan(ctx, deps.Workflow, workflow.ScanInput{Ref: seriesRef})
		// Three-branch IsTracked handler per design/async-job.md § 11.b.
		if !j.IsTracked() {
			_, waitErr := j.Wait(ctx)
			if waitErr != nil {
				// Pre-resolved failure (e.g. cross-kind busy via JobBusyError).
				return toolErrorResult(waitErr), nil, nil
			}
			// Untracked-success: workflow short-circuited the closure. Bug.
			return toolErrorResult(fmt.Errorf("internal: kura_scan returned untracked success (workflow bug)")), nil, nil
		}
		return nil, jobHandleOutput{JobID: j.ID()}, nil
	})
}
