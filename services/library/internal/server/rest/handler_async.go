package rest

import (
	"net/http"
	"time"

	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/workflow"
)

// jobSubmissionResponse is the 202 body for every async submission.
type jobSubmissionResponse struct {
	JobID       string    `json:"jobId"`
	Kind        string    `json:"kind"`
	StatusURL   string    `json:"statusUrl"`
	StreamURL   string    `json:"streamUrl"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// scanRequest is the POST /api/v1/series/{ref}/scan body.
type scanRequest struct {
	Refresh      bool   `json:"refresh,omitempty"`
	MetadataOnly bool   `json:"metadataOnly,omitempty"`
	Ordering     string `json:"ordering,omitempty"`
}

// applyRequest is the POST /api/v1/series/{ref}/reconcile/apply body.
type applyRequest struct {
	Token string `json:"token"`
}

// stageRequest is the POST /api/v1/series/{ref}/stage body. Mirrors
// workflow.StageInput; transports parse domain refs at the boundary.
type stageRequest struct {
	Episodes []stageEpisode `json:"episodes,omitempty"`
	Trash    []stageTrash   `json:"trash,omitempty"`
	Extras   []stageExtra   `json:"extras,omitempty"`
}

type stageEpisode struct {
	Episode    string            `json:"episode"`
	Media      string            `json:"media"`
	Source     string            `json:"source,omitempty"`
	Companions []string          `json:"companions,omitempty"`
	Replace    bool              `json:"replace,omitempty"`
	Attrs      map[string]string `json:"attrs,omitempty"`
}

type stageTrash struct {
	Path       string   `json:"path"`
	Companions []string `json:"companions,omitempty"`
}

type stageExtra struct {
	Season int    `json:"season"`
	Source string `json:"source"`
	Prefix string `json:"prefix,omitempty"`
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req scanRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r.Body, &req); err != nil {
			writeError(w, err)
			return
		}
	}
	job := workflow.Scan(r.Context(), s.deps.Workflow, workflow.ScanInput{
		Ref:          ref,
		Refresh:      req.Refresh,
		MetadataOnly: req.MetadataOnly,
		Ordering:     req.Ordering,
	})
	writeJobAck(w, job.ID(), string(jobs.KindScan), job.StartedAt())
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req applyRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	if req.Token == "" {
		writeError(w, &validationError{msg: "token is required"})
		return
	}
	job := workflow.ApplyReconcile(r.Context(), s.deps.Workflow, workflow.ApplyReconcileInput{
		Ref:   ref,
		Token: req.Token,
	})
	writeJobAck(w, job.ID(), string(jobs.KindReconcileApply), job.StartedAt())
}

func (s *Server) handleStage(w http.ResponseWriter, r *http.Request) {
	ref, err := s.resolveRefPath(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	var req stageRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	in, err := workflow.BuildStageInput(stageRequestToWorkflow(req))
	if err != nil {
		writeError(w, &validationError{msg: err.Error()})
		return
	}
	in.Ref = ref
	job := workflow.Stage(r.Context(), s.deps.Workflow, in)
	writeJobAck(w, job.ID(), string(jobs.KindStage), job.StartedAt())
}

// stageRequestToWorkflow flattens the wire-shape stageRequest into the
// transport-neutral workflow.StageRequest. One field-by-field copy
// per axis; the workflow side owns selector + episode-marker parsing.
func stageRequestToWorkflow(req stageRequest) workflow.StageRequest {
	out := workflow.StageRequest{
		Episodes: make([]workflow.StageRequestEpisode, 0, len(req.Episodes)),
		Trash:    make([]workflow.StageRequestTrash, 0, len(req.Trash)),
		Extras:   make([]workflow.StageRequestExtra, 0, len(req.Extras)),
	}
	for _, ep := range req.Episodes {
		out.Episodes = append(out.Episodes, workflow.StageRequestEpisode{
			Episode:    ep.Episode,
			Media:      ep.Media,
			Source:     ep.Source,
			Companions: ep.Companions,
			Replace:    ep.Replace,
			Attrs:      ep.Attrs,
		})
	}
	for _, t := range req.Trash {
		out.Trash = append(out.Trash, workflow.StageRequestTrash{
			Path:       t.Path,
			Companions: t.Companions,
		})
	}
	for _, ex := range req.Extras {
		out.Extras = append(out.Extras, workflow.StageRequestExtra{
			Season: ex.Season,
			Source: ex.Source,
			Prefix: ex.Prefix,
		})
	}
	return out
}

// writeJobAck emits the 202-Accepted submission response with status
// + stream URL pointers so the client can pick its preferred mode.
func writeJobAck(w http.ResponseWriter, id, kind string, submittedAt time.Time) {
	w.Header().Set(headerJobID, id)
	writeJSON(w, http.StatusAccepted, jobSubmissionResponse{
		JobID:       id,
		Kind:        kind,
		StatusURL:   "/api/v1/jobs/" + id,
		StreamURL:   "/api/v1/jobs/" + id + "/stream",
		SubmittedAt: submittedAt,
	})
}
