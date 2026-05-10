package rest

import (
	"net/http"
	"time"
)

// healthResponse is the GET /api/v1/health body. Liveness probe that
// also surfaces server identity for clients (CLI, WebUI) deciding
// whether to talk to this instance.
type healthResponse struct {
	Ok          bool      `json:"ok"`
	Version     string    `json:"version"`
	LibraryRoot string    `json:"libraryRoot"`
	UptimeMs    int64     `json:"uptimeMs"`
	StartedAt   time.Time `json:"startedAt"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Ok:          true,
		Version:     s.deps.Version,
		LibraryRoot: s.deps.Workflow.LibRoot,
		UptimeMs:    time.Since(s.startedAt).Milliseconds(),
		StartedAt:   s.startedAt,
	})
}
