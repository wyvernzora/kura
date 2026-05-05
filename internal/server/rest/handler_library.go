package rest

import (
	"net/http"
	"time"
)

// libraryResponse is the GET /api/v1/library body. Server-level
// summary independent of any series. Used by WebUI dashboard chrome
// + CLI library introspection.
type libraryResponse struct {
	LibraryRoot string    `json:"libraryRoot"`
	SeriesCount int       `json:"seriesCount"`
	StartedAt   time.Time `json:"startedAt"`
	UptimeMs    int64     `json:"uptimeMs"`
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	count := 0
	if s.deps.Workflow.Index != nil {
		count = s.deps.Workflow.Index.Len()
	}
	writeJSON(w, http.StatusOK, libraryResponse{
		LibraryRoot: s.deps.Workflow.LibRoot,
		SeriesCount: count,
		StartedAt:   s.startedAt,
		UptimeMs:    time.Since(s.startedAt).Milliseconds(),
	})
}
