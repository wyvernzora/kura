// Package health serves the gateway's two health surfaces: its own liveness
// and the synthesized product-level view of the suite.
package health

import (
	"context"
	"encoding/json"
	"io"
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
			results <- result{name: leaf.Name, comp: h.probe(ctx, leaf)}
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

// probe reports a leaf's status. It never surfaces the underlying error: those
// carry hostnames, connection strings, and driver text, none of which belong
// on a product endpoint.
func (h *Handler) probe(ctx context.Context, leaf Leaf) Component {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, leaf.URL, http.NoBody)
	if err != nil {
		return Component{Status: StatusDegraded}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return Component{Status: StatusDegraded}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Component{Status: StatusDegraded}
	}
	// The body is small by contract; cap it anyway so a misrouted response
	// cannot be read unbounded into the health path.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return Component{Status: StatusDegraded}
	}
	var body leafHealth
	if err := json.Unmarshal(raw, &body); err != nil || !body.Ok {
		return Component{Status: StatusDegraded}
	}
	return Component{Status: StatusOK, Version: body.Version}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
