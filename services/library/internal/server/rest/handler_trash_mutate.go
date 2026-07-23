package rest

import (
	"fmt"
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// handleTrashRestore serves POST /api/v1/series/{ref}/trash/{ulid}/restore.
// Operator-only; gated in router.go.
func (s *Server) handleTrashRestore(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := ulid.Parse(r.PathValue("ulid"))
	if err != nil {
		writeError(w, &validationError{msg: fmt.Sprintf("ulid: %v", err)})
		return
	}
	result, werr := workflow.TrashRestore(r.Context(), s.deps.Workflow, workflow.TrashRestoreInput{
		Ref: ref,
		ID:  id,
	})
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleTrashEmptySeries serves DELETE /api/v1/series/{ref}/trash.
// Operator-only + X-Confirm: 1 required.
func (s *Server) handleTrashEmptySeries(w http.ResponseWriter, r *http.Request) {
	if err := requireConfirm(r); err != nil {
		writeError(w, err)
		return
	}
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
	result, werr := workflow.TrashEmpty(r.Context(), s.deps.Workflow, workflow.TrashEmptyInput{
		Ref:       ref,
		OlderThan: older,
	})
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleTrashEmptyAll serves DELETE /api/v1/trash. Library-wide
// destructive; operator-only + X-Confirm: 1 required.
func (s *Server) handleTrashEmptyAll(w http.ResponseWriter, r *http.Request) {
	if err := requireConfirm(r); err != nil {
		writeError(w, err)
		return
	}
	older, err := parseOlderThan(r.URL.Query().Get("olderThan"))
	if err != nil {
		writeError(w, err)
		return
	}
	result, werr := workflow.TrashEmpty(r.Context(), s.deps.Workflow, workflow.TrashEmptyInput{
		All:       true,
		OlderThan: older,
	})
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// requireConfirm enforces X-Confirm: 1 on destructive operations as a
// belt-and-suspenders check beyond the operator-gate. Returns
// validationError on absence; CLI sets the header by default.
func requireConfirm(r *http.Request) error {
	if r.Header.Get(headerConfirm) != "1" {
		return &validationError{msg: "destructive operation requires X-Confirm: 1 header"}
	}
	return nil
}
