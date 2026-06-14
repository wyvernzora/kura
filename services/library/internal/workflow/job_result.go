package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/reconcile"
	"github.com/wyvernzora/kura/internal/response"
)

// ProjectJobResultJSON maps an async workflow's persisted job result
// into the public response shape for agent-facing transports.
func ProjectJobResultJSON(kind string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if kind != string(jobs.KindReconcileApply) {
		out := make(json.RawMessage, len(raw))
		copy(out, raw)
		return out, nil
	}
	var result reconcile.ApplyResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("project reconcile apply job result: %w", err)
	}
	projected := ApplyReconcileResponse(result)
	return json.Marshal(projected)
}

// ReconcileApplyJobResult decodes a projected reconcile-apply job
// result. The boolean is false when raw is empty or belongs to another
// job kind.
func ReconcileApplyJobResult(kind string, raw json.RawMessage) (response.ReconcileApply, bool, error) {
	if len(raw) == 0 || kind != string(jobs.KindReconcileApply) {
		return response.ReconcileApply{}, false, nil
	}
	var result reconcile.ApplyResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return response.ReconcileApply{}, false, fmt.Errorf("decode reconcile apply job result: %w", err)
	}
	return ApplyReconcileResponse(result), true, nil
}
