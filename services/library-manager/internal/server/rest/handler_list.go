package rest

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
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
//	airing             — when "1"/"true"/"yes"/"on", admit only airing
//	                     rows; when "0"/"false"/"no"/"off", admit only
//	                     non-airing rows; absent means no filter.
//	tags               — space-delimited conjunctive tag expressions;
//	                     plain tags require presence, !tag requires absence.
//	limit              — page cap (default 100, max 1000).
//	cursor             — opaque pagination token from a prior response.
//
// Response: api.ListResult, with content-derived ETag.
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

	airing, err := parseOptionalBool(q.Get("airing"), "airing")
	if err != nil {
		writeError(w, err)
		return
	}
	tags := parseTagQuery(q["tags"])

	result, err := workflow.List(r.Context(), s.deps.Workflow, workflow.ListInput{
		Statuses:   statuses,
		Airing:     airing,
		Tags:       tags,
		MaxResults: maxResults,
		Cursor:     q.Get("cursor"),
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSONWithETag(w, r, http.StatusOK, result)
}

func parseTagQuery(values []string) []string {
	var tags []string
	for _, value := range values {
		tags = append(tags, strings.Fields(value)...)
	}
	return tags
}

// parseOptionalBool returns nil for empty input, *true / *false for
// recognized truthy / falsy strings, and a validation error otherwise.
// Truthy: 1 true TRUE yes on. Falsy: 0 false FALSE no off.
func parseOptionalBool(raw, name string) (*bool, error) {
	if raw == "" {
		return nil, nil
	}
	switch raw {
	case "1", "true", "TRUE", "yes", "on":
		v := true
		return &v, nil
	case "0", "false", "FALSE", "no", "off":
		v := false
		return &v, nil
	}
	return nil, &validationError{msg: fmt.Sprintf("%s must be 1/true/yes/on or 0/false/no/off", name)}
}

// parseListStatuses validates each entry against the closed allowed
// set. Unknown values yield 400 invalid_ref.
func parseListStatuses(raw []string) ([]api.ListStatus, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]api.ListStatus, 0, len(raw))
	for _, s := range raw {
		status := api.ListStatus(s)
		if !isAllowedListStatus(status) {
			return nil, &validationError{
				msg: fmt.Sprintf("unknown status %q (allowed: complete, incomplete, error, untracked)", s),
			}
		}
		out = append(out, status)
	}
	return out, nil
}

func isAllowedListStatus(s api.ListStatus) bool {
	switch s {
	case api.ListStatusComplete,
		api.ListStatusIncomplete,
		api.ListStatusError,
		api.ListStatusUntracked:
		return true
	}
	return false
}
