package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/services/library/internal/jobs"
	"github.com/wyvernzora/kura/services/library/internal/workflow"
)

// scanAllRequest is the POST /api/v1/library/scan body. All fields are
// optional; the empty body shape kicks off a default-concurrency scan.
type scanAllRequest struct {
	Refresh      bool `json:"refresh,omitempty"`
	MetadataOnly bool `json:"metadataOnly,omitempty"`
	Concurrency  int  `json:"concurrency,omitempty"`
}

// handleScanAll serves POST /api/v1/library/scan. Job-shaped: returns
// a JobAck the caller can poll via /jobs/{id} or stream via
// /jobs/{id}/stream. The fan-out runs inside the job goroutine.
//
// Submitting while another library-wide job (Reindex / ScanAll) is
// running yields *jobs.JobBusyError → 409 with the running job's
// handle.
func (s *Server) handleScanAll(w http.ResponseWriter, r *http.Request) {
	var req scanAllRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r.Body, &req); err != nil {
			writeError(w, err)
			return
		}
	}
	job := workflow.ScanAll(r.Context(), s.deps.Workflow, workflow.ScanAllInput{
		Refresh:      req.Refresh,
		MetadataOnly: req.MetadataOnly,
		Concurrency:  req.Concurrency,
	})
	writeJobAck(w, job.ID(), string(jobs.KindScanAll), job.StartedAt())
}
