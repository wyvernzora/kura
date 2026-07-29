package rest

import (
	"log/slog"
	"net/http"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

func (h *Handler) handleSubmit(w http.ResponseWriter, r *http.Request) {
	body, ok := h.requirePost(w, r)
	if !ok {
		h.metrics.Submit("invalid", "error", nil)
		h.log(r, slog.LevelDebug, "submit rejected", "reason", "method_or_body")
		return
	}
	var req api.SubmitRequest
	if err := decodeJSON(body, &req); err != nil {
		h.metrics.Submit("invalid", "error", nil)
		h.log(r, slog.LevelInfo, "submit rejected", "reason", "invalid_body", "err", err)
		writeInvalidRequest(w, "invalid request body", nil)
		return
	}
	if err := h.dispatch.SubmitTyped(r.Context(), req); err != nil {
		result := "error"
		if code := dispatch.ErrorKind(err); code == "no_active_lease" || code == "stale_lease" {
			result = "conflict"
		}
		h.metrics.Submit(string(req.Status), result, nil)
		h.log(r, dispatchLogLevel(err), "submit failed",
			"infohash", req.Infohash,
			"claim_token", req.ClaimToken,
			"status", req.Status,
			"ref", req.Ref,
			"has_confidence", req.Confidence != nil,
			"reason_len", len(req.Reason),
			"result", result,
			"code", dispatch.ErrorKind(err),
			"err", err,
		)
		h.writeDispatchError(w, req.Infohash, err)
		return
	}
	h.metrics.Submit(string(req.Status), "ok", req.Confidence)
	h.metrics.UnmatchedReason(string(req.Status), req.Reason)
	h.log(r, slog.LevelInfo, "submit completed",
		"infohash", req.Infohash,
		"claim_token", req.ClaimToken,
		"status", req.Status,
		"ref", req.Ref,
		"has_confidence", req.Confidence != nil,
		"reason_len", len(req.Reason),
	)
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}
