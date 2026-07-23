package rest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// handleTrashListSeries serves GET /api/v1/series/{ref}/trash.
func (s *Server) handleTrashListSeries(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	older, err := parseOlderThan(r.URL.Query().Get("olderThan"))
	if err != nil {
		writeError(w, err)
		return
	}
	result, terr := workflow.TrashList(r.Context(), s.deps.Workflow, workflow.TrashListInput{
		Ref:       ref,
		OlderThan: older,
	})
	if terr != nil {
		writeError(w, terr)
		return
	}
	writeJSONWithETag(w, r, http.StatusOK, result)
}

// handleTrashListAll serves GET /api/v1/trash (library-wide).
func (s *Server) handleTrashListAll(w http.ResponseWriter, r *http.Request) {
	older, err := parseOlderThan(r.URL.Query().Get("olderThan"))
	if err != nil {
		writeError(w, err)
		return
	}
	result, terr := workflow.TrashList(r.Context(), s.deps.Workflow, workflow.TrashListInput{
		All:       true,
		OlderThan: older,
	})
	if terr != nil {
		writeError(w, terr)
		return
	}
	writeJSONWithETag(w, r, http.StatusOK, result)
}

// parseOlderThan parses a Go time.Duration string. Empty = no filter.
// Returns 400 invalid_ref on malformed input.
func parseOlderThan(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, &validationError{msg: fmt.Sprintf("olderThan: %v", err)}
	}
	if d < 0 {
		return 0, &validationError{msg: "olderThan must be non-negative"}
	}
	return d, nil
}
