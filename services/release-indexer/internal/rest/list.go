package rest

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
)

// handleListReleases serves GET /api/releases/v1: releases newest first,
// optionally narrowed to one ref. The store owns the limit default and cap, so
// an omitted limit is passed through as zero rather than guessed at here.
//
// `since` switches the query to the delta path (page by first-matched time)
// rather than the catalog path; that choice lives in the store too.
//
// `status` and `maxConfidence` are the operator surface's filters: an omitted
// `status` keeps the matched-only default the pipeline consumers depend on, and
// the confidence ceiling is the caller's policy — the indexer has no threshold
// of its own. Both are shape-parsed here and meaning-checked in dispatch.
func (h *Handler) handleListReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.log(r, slog.LevelDebug, "list releases rejected", "reason", "method_not_allowed", "method", r.Method)
		writeMethodNotAllowed(w)
		return
	}

	q := r.URL.Query()
	req := dispatch.ListReleasesRequest{
		Ref:    q.Get("ref"),
		Cursor: q.Get("cursor"),
	}
	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 0 {
			h.log(r, slog.LevelDebug, "list releases rejected", "reason", "invalid_limit", "limit", raw)
			writeInvalidRequest(w, "limit must be a non-negative integer", nil)
			return
		}
		req.Limit = limit
	}
	if raw := q.Get("since"); raw != "" {
		since, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			h.log(r, slog.LevelDebug, "list releases rejected", "reason", "invalid_since", "since", raw)
			writeInvalidRequest(w, "since must be an RFC3339 timestamp", nil)
			return
		}
		req.Since = &since
	}
	if raw := q.Get("status"); raw != "" {
		req.Statuses = strings.Split(raw, ",")
	}
	if raw := q.Get("maxConfidence"); raw != "" {
		maxConfidence, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			h.log(r, slog.LevelDebug, "list releases rejected", "reason", "invalid_max_confidence", "maxConfidence", raw)
			writeInvalidRequest(w, "maxConfidence must be a number in (0,1]", nil)
			return
		}
		req.MaxConfidence = &maxConfidence
	}

	out, err := h.dispatch.ListReleasesTyped(r.Context(), req)
	if err != nil {
		h.log(r, dispatchLogLevel(err), "list releases failed",
			"ref", req.Ref,
			"code", dispatch.ErrorKind(err),
			"err", err,
		)
		h.writeDispatchError(w, "", err)
		return
	}
	h.log(r, slog.LevelInfo, "list releases completed", "ref", req.Ref, "count", len(out.Items))
	writeJSONValue(w, http.StatusOK, out)
}
