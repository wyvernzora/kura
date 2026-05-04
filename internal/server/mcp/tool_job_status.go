package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/errkind"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

type jobStatusInput struct {
	JobID string `json:"jobId" jsonschema:"Job ID returned by kura_scan or kura_reconcile_apply (16-char hex)."`
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

var jobIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

const jobStatusDescription = `Look up the current status of an async job submitted by ` + "`kura_scan`" + ` or ` + "`kura_reconcile_apply`" + `. Returns the job's state, latest progress event, and on terminal state either the workflow result or the failure reason.

` + "`state`" + ` values:
- ` + "`running`" + `: job is in flight; ` + "`progress`" + ` is the latest emitted event (may be absent if the job hasn't emitted yet).
- ` + "`succeeded`" + `: job finished; ` + "`result`" + ` carries the workflow's typed return.
- ` + "`failed`" + `: job finished with an error; ` + "`error`" + ` carries kind + message + structured data.

Polling cadence is up to the caller. Jobs are retained for 5 minutes after terminal state; a lookup past that horizon returns ` + "`not_found`" + `.`

func addJobStatusTool(s *sdkmcp.Server, deps Deps) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "kura_job_status",
		Title:       "Get async job status",
		Description: jobStatusDescription,
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:    true,
			IdempotentHint:  true,
			OpenWorldHint:   &hintFalse,
			DestructiveHint: &hintFalse,
		},
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, in jobStatusInput) (*sdkmcp.CallToolResult, any, error) {
		if !jobIDPattern.MatchString(in.JobID) {
			return toolErrorResult(&invalidInputError{
				kind:    errkind.KindInvalidRef,
				message: fmt.Sprintf("kura_job_status: invalid jobId %q; expected 16-char hex", in.JobID),
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

// lookupMetadataRef walks the index and returns the metadata ref
// tracking series. Empty string when the series isn't (or no longer
// is) tracked — happens if the series got removed mid-job.
func lookupMetadataRef(idx *indexfile.Index, series refs.Series) string {
	for _, entry := range idx.Entries() {
		if entry.Series == series {
			return entry.Metadata.String()
		}
	}
	return ""
}

// avoid unused-import warning when Result is empty in some test paths.
var _ = progress.Event{}
