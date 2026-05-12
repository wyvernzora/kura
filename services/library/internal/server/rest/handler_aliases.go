package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

// handleAliasesList serves GET /api/v1/series/{ref}/aliases. Returns
// the persisted user aliases for the addressed series. TVDB-derived
// aliases never appear here — they're folded into searchKey at scan
// time and discarded.
func (s *Server) handleAliasesList(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	out, werr := workflow.ListUserAliases(r.Context(), s.deps.Workflow, ref)
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAliasesAdd serves POST /api/v1/series/{ref}/aliases. Appends
// each non-empty entry to the series's UserAliases (deduped),
// recomputes searchKey, and persists. Idempotent — adding an existing
// alias is a no-op.
func (s *Server) handleAliasesAdd(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req response.UserAliasMutation
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	out, werr := workflow.AddUserAliases(r.Context(), s.deps.Workflow, ref, req.Aliases)
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAliasesRemove serves DELETE /api/v1/series/{ref}/aliases.
// Drops each entry from UserAliases. Removing a missing alias is a
// no-op.
func (s *Server) handleAliasesRemove(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req response.UserAliasMutation
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	out, werr := workflow.RemoveUserAliases(r.Context(), s.deps.Workflow, ref, req.Aliases)
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
