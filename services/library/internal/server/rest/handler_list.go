package rest

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/workflow"
)

const (
	defaultListMaxResults = 100
	maxListMaxResults     = 1000
)

// handleList serves GET /api/v1/series.
//
// Query:
//
//	status (repeatable) — filter by ListStatus.
//	limit              — page cap (default 100, max 1000).
//	cursor             — opaque pagination token from a prior response.
//
// Response: response.ListResult, with content-derived ETag.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statuses, err := parseListStatuses(q["status"])
	if err != nil {
		writeError(w, err)
		return
	}

	maxResults := defaultListMaxResults
	if v := q.Get("limit"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 0 {
			writeError(w, &validationError{msg: "limit must be a non-negative integer"})
			return
		}
		switch {
		case n == 0:
			maxResults = defaultListMaxResults
		case n > maxListMaxResults:
			maxResults = maxListMaxResults
		default:
			maxResults = n
		}
	}

	result, err := workflow.List(r.Context(), s.deps.Workflow, workflow.ListInput{
		Statuses:   statuses,
		MaxResults: maxResults,
		Cursor:     q.Get("cursor"),
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSONWithETag(w, r, http.StatusOK, result)
}

// parseListStatuses validates each entry against the closed allowed
// set. Unknown values yield 400 invalid_ref.
func parseListStatuses(raw []string) ([]response.ListStatus, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]response.ListStatus, 0, len(raw))
	for _, s := range raw {
		status := response.ListStatus(s)
		if !isAllowedListStatus(status) {
			return nil, &validationError{
				msg: fmt.Sprintf("unknown status %q (allowed: complete, incomplete, airing, error, untracked)", s),
			}
		}
		out = append(out, status)
	}
	return out, nil
}

func isAllowedListStatus(s response.ListStatus) bool {
	switch s {
	case response.ListStatusComplete,
		response.ListStatusIncomplete,
		response.ListStatusAiring,
		response.ListStatusError,
		response.ListStatusUntracked:
		return true
	}
	return false
}
