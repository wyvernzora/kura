package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/internal/workflow"
)

// handleReindex serves POST /api/v1/library/reindex. Operator-only.
// Returns 204 on success; reindex has no response body.
func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	if err := workflow.Reindex(r.Context(), s.deps.Workflow); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
