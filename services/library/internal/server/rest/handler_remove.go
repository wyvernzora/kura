package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// handleRemove serves DELETE /api/v1/series/{ref}.
//
// Query: purge=1 makes the call full-delete (operator-only, gated by
// the operator middleware in router.go).
func (s *Server) handleRemove(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	purge := r.URL.Query().Get("purge") == "1"
	result, werr := workflow.Remove(r.Context(), s.deps.Workflow, workflow.RemoveInput{
		Ref:   ref,
		Purge: purge,
	})
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
