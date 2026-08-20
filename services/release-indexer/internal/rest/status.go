package rest

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/internal/infohash"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// handleSetStatus serves PUT /api/releases/v1/{infohash}/status. The store's
// transition table decides what is allowed; a rejected transition comes back
// as 409 naming both ends of it.
func (h *Handler) handleSetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		h.log(r, slog.LevelDebug, "set status rejected", "reason", "method_not_allowed", "method", r.Method)
		writeMethodNotAllowed(w)
		return
	}

	ih, err := infohash.NormalizeInfohash(r.PathValue("infohash"))
	if err != nil {
		h.log(r, slog.LevelDebug, "set status rejected", "reason", "invalid_infohash")
		writeInvalidRequest(w, "invalid infohash", nil)
		return
	}

	body, err := readAll(r)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, api.KindInvalidRequest,
				"request body exceeds limit", map[string]any{"limitBytes": maxRequestBytes})
			return
		}
		writeInvalidRequest(w, "unreadable request body", nil)
		return
	}
	var req api.SetStatusRequest
	if err := decodeJSON(body, &req); err != nil {
		h.log(r, slog.LevelInfo, "set status rejected", "reason", "invalid_body", "err", err)
		writeInvalidRequest(w, "invalid request body", nil)
		return
	}

	if err := h.dispatch.SetStatusTyped(r.Context(), ih, req); err != nil {
		h.log(r, dispatchLogLevel(err), "set status failed",
			"infohash", ih,
			"status", req.Status,
			"reason_len", len(req.Reason),
			"code", dispatch.ErrorKind(err),
			"err", err,
		)
		h.writeDispatchError(w, ih, err)
		return
	}
	h.log(r, slog.LevelInfo, "set status completed",
		"infohash", ih,
		"status", req.Status,
		"reason_len", len(req.Reason),
	)
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}
