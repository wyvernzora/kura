package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/services/library/internal/reconcile"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// recoverRequest is the POST /api/v1/series/{ref}/reconcile/recover body.
type recoverRequest struct {
	Force bool `json:"force,omitempty"`
}

func (s *Server) handleReconcilePlan(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	result, werr := workflow.PlanReconcile(r.Context(), s.deps.Workflow, reconcile.PlanInput{Ref: ref})
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleReconcileRecover(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req recoverRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r.Body, &req); err != nil {
			writeError(w, err)
			return
		}
	}
	result, werr := workflow.RecoverReconcile(r.Context(), s.deps.Workflow, reconcile.RecoverInput{
		Ref:   ref,
		Force: req.Force,
	})
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
