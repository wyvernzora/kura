package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// resolveRequest is the POST /api/v1/resolve body.
type resolveRequest struct {
	Terms []string `json:"terms"`
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var req resolveRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	if len(req.Terms) == 0 {
		writeError(w, &validationError{msg: "terms is required and must be non-empty"})
		return
	}
	result, err := workflow.Resolve(r.Context(), s.deps.Workflow, workflow.ResolveInput{Terms: req.Terms})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
