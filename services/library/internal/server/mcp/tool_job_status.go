package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

type jobStatusInput struct {
	JobID string `json:"jobId" jsonschema:"Job ID returned by kura_scan or kura_reconcile_apply (26-char Crockford base32 ULID)."`
}

type mcpJobStatus struct {
	JobID       string          `json:"jobId"`
	Kind        string          `json:"kind"`
	MetadataRef string          `json:"metadataRef,omitempty"`
	State       string          `json:"state"`
	StartedAt   string          `json:"startedAt"`
	EndedAt     string          `json:"endedAt,omitempty"`
	Progress    *mcpJobProgress `json:"progress,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *mcpJobError    `json:"error,omitempty"`
}

type mcpJobProgress struct {
	Phase   string `json:"phase"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

type mcpJobError struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

//go:embed tool_job_status.md
var toolJobStatusDoc string

func addJobStatusTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_job_status",
		Title:       "Get async job status",
		Description: forLLM(toolJobStatusDoc),
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in jobStatusInput) (*sdkmcp.CallToolResult, any, error) {
		if _, err := ulid.ParseStrict(in.JobID); err != nil {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_job_status: invalid jobId %q; expected 26-char Crockford base32 ULID", in.JobID),
			}), nil, nil
		}
		view, err := deps.Workflow.Jobs.Get(in.JobID)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, projectJobStatus(view, deps.Workflow.Index), nil
	})
}

// projectJobStatus turns the registry's UntypedJob view into the
// MCP wire shape, swapping the internal SeriesRef for the agent-
// facing MetadataRef via index reverse-lookup.
func projectJobStatus(view jobs.UntypedJob, idx *indexfile.Index) mcpJobStatus {
	out := mcpJobStatus{
		JobID:       view.ID(),
		Kind:        view.Kind(),
		MetadataRef: lookupMetadataRef(idx, view.Series()),
		State:       view.State().String(),
		StartedAt:   view.StartedAt().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if endedAt, ok := view.EndedAt(); ok {
		out.EndedAt = endedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if ev := view.Progress(); ev != nil {
		out.Progress = &mcpJobProgress{
			Phase:   ev.Stage,
			Status:  string(ev.Status),
			Message: ev.Message,
			Current: ev.Current,
			Total:   ev.Total,
		}
	}
	if raw := view.Result(); len(raw) > 0 {
		out.Result = raw
	}
	if err := view.Err(); err != nil {
		out.Error = projectJobError(err)
	}
	return out
}

// projectJobError extracts kind/message/data from a terminal-failure
// error. Coded errors (errkind.Coded) carry their own taxonomy;
// shutdown sentinel and untyped errors fall back to "internal".
func projectJobError(err error) *mcpJobError {
	if jobs.IsShutdownError(err) {
		return &mcpJobError{
			Kind:    errkind.KindInternal,
			Message: err.Error(),
		}
	}
	if coded, ok := errors.AsType[errkind.Coded](err); ok {
		return &mcpJobError{
			Kind:    coded.Kind(),
			Message: err.Error(),
			Data:    coded.Data(),
		}
	}
	return &mcpJobError{
		Kind:    errkind.KindInternal,
		Message: err.Error(),
	}
}

// lookupMetadataRef returns the metadata ref tracking series via
// the index's O(1) reverse lookup. Empty string when the series
// isn't (or no longer is) tracked — happens if the series got
// removed mid-job.
func lookupMetadataRef(idx *indexfile.Index, series refs.Series) string {
	row, ok := idx.GetRow(series)
	if !ok {
		return ""
	}
	return row.Metadata.String()
}

// avoid unused-import warning when Result is empty in some test paths.
var _ = progress.Event{}
