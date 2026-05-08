package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/workflow"
)

// handleReindex serves POST /api/v1/library/reindex. Operator-only.
// Job-shaped: returns a JobAck the caller can stream via
// /jobs/{id}/stream. The walk + index write happen inside the job
// goroutine; progress events flow through the registry's reporter.
func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	job := workflow.Reindex(r.Context(), s.deps.Workflow)
	writeJobAck(w, job.ID(), string(jobs.KindReindex), job.StartedAt())
}
