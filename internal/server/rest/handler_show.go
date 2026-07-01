package rest

import (
	"fmt"
	"net/http"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

// handleShow serves GET /api/v1/series/{ref}.
//
// {ref} is a MetadataRef (provider:id). Per Product.md "Selectors,
// not paths" every resource endpoint identifies series by metadata
// ref; the server resolves to the storage SeriesRef via the index.
// SeriesRef in the path is rejected.
//
// Query: episodes, status (csv), source (csv), resolution (csv).
//
// Response: response.Show, content-derived ETag.
func (s *Server) handleShow(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// preview=true renders a not-yet-added series from live provider
	// metadata, addressed directly by metadata ref (no index lookup).
	preview := q.Get("preview") == "true" || q.Get("preview") == "1"
	var in workflow.ShowInput
	if preview {
		metaRef, err := refs.ParseMetadata(r.PathValue("ref"))
		if err != nil {
			writeError(w, &validationError{
				msg: fmt.Sprintf("invalid metadata ref %q (expected provider:id, e.g. tvdb:370070): %v", r.PathValue("ref"), err),
			})
			return
		}
		in = workflow.ShowInput{Preview: true, MetadataRef: metaRef}
	} else {
		ref, err := s.resolveRefPath(r.PathValue("ref"))
		if err != nil {
			writeError(w, err)
			return
		}
		in = workflow.ShowInput{Ref: ref}
	}

	if raw := q.Get("episodes"); raw != "" {
		sel, perr := refs.ParseEpisodeSelector(raw)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("episodes: %v", perr)})
			return
		}
		in.Episodes = sel
	}

	in.Status = parseStatusFilter(q["status"])
	in.Source = q["source"]
	in.Resolution = q["resolution"]

	result, err := workflow.Show(r.Context(), s.deps.Workflow, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSONWithETag(w, r, http.StatusOK, result)
}

// resolveRefPath parses {ref} as a MetadataRef and resolves to the
// SeriesRef via the index. Per Product.md "Selectors, not paths"
// every resource endpoint identifies series by metadata ref;
// SeriesRef is internal-only. Returns:
//
//   - 400 invalid_ref when the path doesn't parse as MetadataRef.
//   - 404 not_found (KindNotFound) when the metadata ref isn't
//     tracked by the index (MetadataRefNotIndexedError).
//
// Used by every handler that takes {ref} in a path: show, remove,
// reset, scan, stage, reconcile (plan/apply/recover), trash list/
// restore/empty.
func (s *Server) resolveRefPath(raw string) (refs.Series, error) {
	if raw == "" {
		return refs.Series{}, &validationError{msg: "metadata ref required in path"}
	}
	metaRef, err := refs.ParseMetadata(raw)
	if err != nil {
		return refs.Series{}, &validationError{
			msg: fmt.Sprintf("invalid metadata ref %q (expected provider:id, e.g. tvdb:370070): %v", raw, err),
		}
	}
	seriesRef, ok, lerr := s.deps.Workflow.Index.Get(metaRef)
	if lerr != nil {
		return refs.Series{}, lerr
	}
	if !ok {
		return refs.Series{}, &workflow.MetadataRefNotIndexedError{Ref: metaRef}
	}
	return seriesRef, nil
}

// parseStatusFilter passes through known response.Status strings. Unknown
// values are ignored rather than rejected; the workflow treats them as
// no-op filters anyway, and keeping the surface tolerant is cheaper
// than mirroring the closed enum here.
func parseStatusFilter(raw []string) []response.Status {
	if len(raw) == 0 {
		return nil
	}
	out := make([]response.Status, 0, len(raw))
	for _, s := range raw {
		out = append(out, response.Status(s))
	}
	return out
}
