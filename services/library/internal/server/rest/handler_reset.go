package rest

import (
	"fmt"
	"net/http"

	"github.com/oklog/ulid/v2"

	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// resetRequest is the POST /api/v1/series/{ref}/reset body.
//
// Episode and All are mutually exclusive on the staged-episode side;
// trashIds and extraIds drop staged trash/extras records by ULID.
type resetRequest struct {
	Episode  string   `json:"episode,omitempty"`
	All      bool     `json:"all,omitempty"`
	TrashIDs []string `json:"trashIds,omitempty"`
	ExtraIDs []string `json:"extraIds,omitempty"`
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req resetRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	in := workflow.ResetInput{Ref: ref, All: req.All}
	if req.Episode != "" {
		ep, perr := refs.ParseEpisode(req.Episode)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("episode: %v", perr)})
			return
		}
		in.Episode = ep
	}
	for _, raw := range req.TrashIDs {
		id, perr := ulid.Parse(raw)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("trashIds[%q]: %v", raw, perr)})
			return
		}
		in.TrashIDs = append(in.TrashIDs, id)
	}
	for _, raw := range req.ExtraIDs {
		id, perr := ulid.Parse(raw)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("extraIds[%q]: %v", raw, perr)})
			return
		}
		in.ExtraIDs = append(in.ExtraIDs, id)
	}
	result, werr := workflow.Reset(r.Context(), s.deps.Workflow, in)
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
