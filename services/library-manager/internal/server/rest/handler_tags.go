package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// handleTagsUpdate serves PATCH /api/v1/series/{ref}/tags. Plain tag
// expressions add a tag; expressions prefixed with ! remove it.
func (s *Server) handleTagsUpdate(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req api.TagUpdate
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	out, err := workflow.UpdateTags(r.Context(), s.deps.Workflow, workflow.UpdateTagsInput{
		Ref:  ref,
		Tags: req.Tags,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
