package rest

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/internal/infohash"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

type magnetResponse struct {
	Infohash string `json:"infohash"`
	Magnet   string `json:"magnet"`
}

func (h *Handler) handleGetMagnet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.log(r, slog.LevelDebug, "magnet lookup rejected", "reason", "method_not_allowed", "method", r.Method)
		writeMethodNotAllowed(w)
		return
	}

	raw := r.PathValue("infohash")
	if raw == "" {
		h.log(r, slog.LevelDebug, "magnet lookup rejected", "reason", "invalid_infohash")
		writeInvalidRequest(w, "invalid infohash", nil)
		return
	}

	ih, err := infohash.NormalizeInfohash(raw)
	if err != nil {
		h.log(r, slog.LevelDebug, "magnet lookup rejected", "reason", "invalid_infohash")
		writeInvalidRequest(w, "invalid infohash", nil)
		return
	}
	out, err := h.dispatch.ResolveMagnetsTyped(r.Context(), dispatch.ResolveMagnetsRequest{Infohashes: []string{ih}})
	if err != nil {
		h.log(r, dispatchLogLevel(err), "magnet lookup failed",
			"infohash", ih,
			"code", dispatch.ErrorKind(err),
			"err", err,
		)
		h.writeDispatchError(w, ih, err)
		return
	}
	magnet, ok := out.Magnets[ih]
	if !ok {
		// One extra query, on the miss path only, splits "unknown infohash"
		// from "known but not matched": the pipeline treats those very
		// differently (garbage input vs a selection that went stale, e.g.
		// the pruner marked it dead between list and fetch).
		detail, derr := h.dispatch.GetReleaseTyped(r.Context(), dispatch.GetReleaseRequest{Infohash: ih})
		switch {
		case derr == nil:
			h.log(r, slog.LevelInfo, "magnet lookup gated",
				"infohash", ih, "match_status", detail.MatchStatus)
			writeError(w, http.StatusConflict, api.KindNotMatched,
				fmt.Sprintf("release is %s, not matched", detail.MatchStatus),
				map[string]any{"infohash": ih, "matchStatus": detail.MatchStatus})
		case dispatch.ErrorKind(derr) == api.KindNotFound:
			h.log(r, slog.LevelInfo, "magnet lookup missed", "infohash", ih)
			writeError(w, http.StatusNotFound, api.KindNotFound, store.ErrNoSuchRelease.Error(),
				map[string]any{"infohash": ih})
		default:
			// A store failure must not be reported as a missing release:
			// the pipeline would drop a release that exists.
			h.log(r, dispatchLogLevel(derr), "magnet lookup failed",
				"infohash", ih,
				"code", dispatch.ErrorKind(derr),
				"err", derr,
			)
			h.writeDispatchError(w, ih, derr)
		}
		return
	}
	h.log(r, slog.LevelInfo, "magnet lookup completed", "infohash", ih)
	writeJSONValue(w, http.StatusOK, magnetResponse{Infohash: ih, Magnet: magnet})
}
