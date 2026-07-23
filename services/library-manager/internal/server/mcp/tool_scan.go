package mcp

import (
	"context"
	_ "embed"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider/tvdb"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

type scanInput struct {
	Ref      string `json:"ref" jsonschema:"Metadata ref, e.g. \"tvdb:370070\"."`
	Refresh  bool   `json:"refresh,omitempty" jsonschema:"Force re-run of mediainfo and source detection on every active record, even when size and mtime are unchanged. A freshly detected Unknown source will not overwrite an existing non-Unknown one. Use after fixing filename source tokens or when the on-disk record's source/resolution looks wrong."`
	Ordering string `json:"ordering,omitempty" jsonschema:"Pin the per-series episode ordering and re-fetch the spine under it. One of: default, official, dvd, absolute, alternate, regional. Omit to keep the series's existing ordering."`
}

type jobHandleOutput struct {
	JobID string `json:"jobId"`
}

//go:embed tool_scan.md
var toolScanDoc string

func addScanTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_scan",
		Title:       "Scan series for media files",
		Description: forLLM(toolScanDoc),
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
		ordering, err := tvdb.ParseOrdering(in.Ordering)
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
		j := workflow.Scan(ctx, deps.Workflow, workflow.ScanInput{Ref: seriesRef, Refresh: in.Refresh, Ordering: ordering})
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
