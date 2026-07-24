package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oklog/ulid/v2"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/progress"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

type jobStatusInput struct {
	JobID         string `json:"jobId" jsonschema:"Job ID returned by kura_scan, kura_stage, or kura_reconcile_apply (26-char Crockford base32 ULID)."`
	IncludeResult bool   `json:"includeResult,omitempty" jsonschema:"When true, include terminal success result payload. Defaults to false for compact polling."`
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
	Kind     string         `json:"kind"`
	Category string         `json:"category"`
	Message  string         `json:"message"`
	Data     map[string]any `json:"data,omitempty"`
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
		out, err := projectJobStatus(view, deps.Workflow.Index, in.IncludeResult)
		if err != nil {
			return toolErrorResult(err), nil, nil
		}
		return nil, out, nil
	})
}

// projectJobStatus turns the registry's UntypedJob view into the
// MCP wire shape, swapping the internal SeriesRef for the agent-
// facing MetadataRef via index reverse-lookup.
func projectJobStatus(view jobs.UntypedJob, idx *indexfile.Index, includeResult bool) (mcpJobStatus, error) {
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
	if includeResult {
		if raw := view.Result(); len(raw) > 0 {
			projected, err := workflow.ProjectJobResultJSON(view.Kind(), raw)
			if err != nil {
				return mcpJobStatus{}, err
			}
			out.Result = projected
		}
	}
	if err := view.Err(); err != nil {
		var detail any
		if raw := view.TerminalResult(); len(raw) > 0 {
			reconcileResult, ok, resultErr := workflow.ReconcileApplyJobResult(view.Kind(), raw)
			if resultErr != nil {
				return mcpJobStatus{}, resultErr
			}
			if ok && hasReconcileApplyDetail(reconcileResult) {
				detail = reconcileResult
			}
		}
		out.Error = projectJobError(err, detail)
	}
	return out, nil
}

// projectJobError extracts kind/message/data from a terminal-failure
// error. Coded errors (errkind.Coded) carry their own taxonomy;
// shutdown sentinel and untyped errors fall back to "internal".
func projectJobError(err error, result any) *mcpJobError {
	if jobs.IsShutdownError(err) {
		return &mcpJobError{
			Kind:     errkind.KindInternal,
			Category: errkind.CategoryInternalError,
			Message:  err.Error(),
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &mcpJobError{
			Kind:     errkind.KindInternal,
			Category: errkind.CategoryCancelled,
			Message:  err.Error(),
		}
	}
	if coded, ok := errors.AsType[errkind.Coded](err); ok {
		data := cloneJobErrorData(coded.Data())
		if result != nil {
			data["result"] = result
		}
		return &mcpJobError{
			Kind:     coded.Kind(),
			Category: coded.Category(),
			Message:  err.Error(),
			Data:     data,
		}
	}
	data := map[string]any(nil)
	if result != nil {
		data = map[string]any{"result": result}
	}
	return &mcpJobError{
		Kind:     errkind.KindInternal,
		Category: errkind.CategoryInternalError,
		Message:  err.Error(),
		Data:     data,
	}
}

func cloneJobErrorData(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func hasReconcileApplyDetail(result api.ReconcileApply) bool {
	return !result.Series.IsZero() ||
		result.AppliedSteps > 0 ||
		result.TotalSteps > 0 ||
		len(result.AppliedStepIDs) > 0 ||
		result.FailedStep != nil
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
