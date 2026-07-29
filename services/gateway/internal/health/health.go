// Package health serves the gateway's two health surfaces: its own liveness
// and the synthesized product-level view of the suite.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Status values used by both surfaces.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// Component is one entry of the health tree.
type Component struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// System is the GET /api/v1/health body.
type System struct {
	Status     string               `json:"status"`
	Version    string               `json:"version"`
	Components map[string]Component `json:"components"`
}

// Local is the GET /healthz body: the gateway's own liveness, with no backend
// dependency. A leaf outage must not restart the gateway Pod.
type Local struct {
	Ok      bool   `json:"ok"`
	Version string `json:"version"`
}

// leafHealth is the shape both leaves return from their own /healthz.
type leafHealth struct {
	Ok      bool   `json:"ok"`
	Version string `json:"version"`
}

// Leaf names a component in the tree and where to probe it.
type Leaf struct {
	Name string
	URL  string
}

// Handler serves both surfaces.
type Handler struct {
	version    string
	leaves     []Leaf
	timeout    time.Duration
	httpClient *http.Client

	// Logger, when non-nil, records why a probe degraded. The wire
	// deliberately hides the cause (hostnames, driver text); the log
	// is where the operator finds it.
	Logger *slog.Logger
}

// New builds the handler. timeout bounds each component probe individually —
// the fan-out is concurrent, so a single slow leaf does not add to the others.
func New(version string, leaves []Leaf, timeout time.Duration) *Handler {
	return &Handler{
		version:    version,
		leaves:     leaves,
		timeout:    timeout,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Local answers /healthz. It deliberately probes nothing: this is what Caddy
// forwards for the Pod's liveness and readiness, and tying it to a leaf would
// let a database outage restart the gateway.
func (h *Handler) Local(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Local{Ok: true, Version: h.version})
}

// System answers /api/v1/health with the synthesized tree.
func (h *Handler) System(w http.ResponseWriter, r *http.Request) {
	out := System{
		Status:  StatusOK,
		Version: h.version,
		Components: map[string]Component{
			// The gateway reports itself without a probe: it is the
			// thing answering.
			"gateway": {Status: StatusOK, Version: h.version},
		},
	}

	type result struct {
		name string
		comp Component
	}
	ctx := r.Context()
	results := make(chan result, len(h.leaves))
	for _, leaf := range h.leaves {
		go func(ctx context.Context, leaf Leaf) {
			comp, err := h.probe(ctx, leaf)
			if err != nil && h.Logger != nil {
				// "leaf", not "component" — the logger already carries
				// component=health, and slog would emit both keys.
				h.Logger.Warn("leaf health probe degraded", "leaf", leaf.Name, "url", leaf.URL, "err", err)
			}
			results <- result{name: leaf.Name, comp: comp}
		}(ctx, leaf)
	}
	for range h.leaves {
		got := <-results
		out.Components[got.name] = got.comp
		if got.comp.Status != StatusOK {
			out.Status = StatusDegraded
		}
	}

	status := http.StatusOK
	if out.Status != StatusOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, out)
}

// probe reports a leaf's status. The returned error never reaches the wire —
// it carries hostnames, connection strings, and driver text, none of which
// belong on a product endpoint — but the caller logs it.
func (h *Handler) probe(ctx context.Context, leaf Leaf) (Component, error) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, leaf.URL, http.NoBody)
	if err != nil {
		return Component{Status: StatusDegraded}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return Component{Status: StatusDegraded}, fmt.Errorf("probe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Component{Status: StatusDegraded}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	// The body is small by contract; cap it anyway so a misrouted response
	// cannot be read unbounded into the health path.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return Component{Status: StatusDegraded}, fmt.Errorf("read body: %w", err)
	}
	var body leafHealth
	if err := json.Unmarshal(raw, &body); err != nil {
		return Component{Status: StatusDegraded}, fmt.Errorf("decode body: %w", err)
	}
	if !body.Ok {
		return Component{Status: StatusDegraded}, fmt.Errorf("leaf reports ok=false")
	}
	return Component{Status: StatusOK, Version: body.Version}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
