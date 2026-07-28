package rest

import (
	"log/slog"
	"net/http"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/internal/infohash"
)

func (h *Handler) handleGetRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.log(r, slog.LevelDebug, "release lookup rejected", "reason", "method_not_allowed", "method", r.Method)
		writeMethodNotAllowed(w)
		return
	}

	raw := r.PathValue("infohash")
	if raw == "" {
		h.log(r, slog.LevelDebug, "release lookup rejected", "reason", "invalid_infohash")
		writeInvalidRequest(w, "invalid infohash", nil)
		return
	}
	ih, err := infohash.NormalizeInfohash(raw)
	if err != nil {
		h.log(r, slog.LevelDebug, "release lookup rejected", "reason", "invalid_infohash")
		writeInvalidRequest(w, "invalid infohash", nil)
		return
	}
	out, err := h.dispatch.GetReleaseTyped(r.Context(), dispatch.GetReleaseRequest{Infohash: ih})
	if err != nil {
		h.log(r, dispatchLogLevel(err), "release lookup failed",
			"infohash", ih,
			"code", dispatch.ErrorKind(err),
			"err", err,
		)
		h.writeDispatchError(w, ih, err)
		return
	}
	h.log(r, slog.LevelInfo, "release lookup completed", "infohash", out.Infohash)
	writeJSONValue(w, http.StatusOK, out)
}
