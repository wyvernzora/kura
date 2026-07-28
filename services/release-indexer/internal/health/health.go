// Package health owns the standalone /healthz handler (design §10/§11).
//
// /healthz is NOT owned by internal/mcp: it is a single mountable http.Handler
// constructed from a Store, so both surfaces can mount the SAME handler — the
// plain net/http listener and the internal/mcp SDK server mount the identical
// handler. Its contract is a single DB-reachability check (design §10, as amended
// by workspace-migration §2): ingestion is external push now, so /healthz no longer
// carries a scrape-recency signal — liveness is purely "can we reach the DB".
package health

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// handler is the /healthz http.Handler. It probes the Store's DB reachability and
// reports 200 when the ping succeeds, non-200 otherwise.
type handler struct {
	store   store.Store
	logger  *slog.Logger
	version string
}

// NewHandler constructs the standalone /healthz handler from the Store (design
// §10/§11). The clock argument the old scrape-recency check needed is gone — the
// probe is a pure DB ping now.
func NewHandler(s store.Store) http.Handler {
	return &handler{store: s}
}

func NewHandlerWithLogger(s store.Store, logger *slog.Logger, version string) http.Handler {
	return &handler{store: s, logger: logger, version: version}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Single readiness check: a live DB round-trip. A closed/unreachable pool fails
	// the ping => non-200; never a bare 200-OK stub (§10/§11).
	// The failure body carries no error text: a ping failure wraps the
	// driver error, which can name the host, database, and user. The
	// status code is the signal; the detail goes to the log.
	if err := h.store.Ping(r.Context()); err != nil {
		if h.logger != nil {
			h.logger.WarnContext(r.Context(), "health check failed", "err", err)
		}
		h.write(w, http.StatusServiceUnavailable, api.Health{Ok: false, Version: h.version})
		return
	}
	h.write(w, http.StatusOK, api.Health{Ok: true, Version: h.version})
}

func (h *handler) write(w http.ResponseWriter, status int, body api.Health) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
