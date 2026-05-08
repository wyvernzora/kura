package rest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/selector"
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
	Episode    string   `json:"episode"`
	Media      string   `json:"media"`
	Source     string   `json:"source,omitempty"`
	Companions []string `json:"companions,omitempty"`
	Replace    bool     `json:"replace,omitempty"`
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
	if len(req.Episodes) == 0 && len(req.Trash) == 0 && len(req.Extras) == 0 {
		writeError(w, &validationError{msg: "at least one of episodes, trash, or extras is required"})
		return
	}
	in := workflow.StageInput{Ref: ref}
	for i, ep := range req.Episodes {
		parsed, perr := refs.ParseEpisode(ep.Episode)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("episodes[%d].episode: %v", i, perr)})
			return
		}
		if ep.Media == "" {
			writeError(w, &validationError{msg: fmt.Sprintf("episodes[%d].media is required (an inbox: selector — see kura_inbox_list)", i)})
			return
		}
		mediaSel, perr := selector.ParseInbox(ep.Media)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("episodes[%d].media: %v", i, perr)})
			return
		}
		companions := make([]selector.Path, 0, len(ep.Companions))
		for j, raw := range ep.Companions {
			sel, cerr := selector.ParseInbox(raw)
			if cerr != nil {
				writeError(w, &validationError{msg: fmt.Sprintf("episodes[%d].companions[%d]: %v", i, j, cerr)})
				return
			}
			companions = append(companions, sel)
		}
		in.Episodes = append(in.Episodes, workflow.EpisodeStageItem{
			Episode:    parsed,
			Media:      mediaSel,
			Source:     ep.Source,
			Companions: companions,
			Replace:    ep.Replace,
		})
	}
	for i, t := range req.Trash {
		if t.Path == "" {
			writeError(w, &validationError{msg: fmt.Sprintf("trash[%d].path is required (a series: selector — relative to the series root)", i)})
			return
		}
		pathSel, perr := selector.ParseSeries(t.Path)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("trash[%d].path: %v", i, perr)})
			return
		}
		companions := make([]selector.Path, 0, len(t.Companions))
		for j, raw := range t.Companions {
			sel, cerr := selector.ParseSeries(raw)
			if cerr != nil {
				writeError(w, &validationError{msg: fmt.Sprintf("trash[%d].companions[%d]: %v", i, j, cerr)})
				return
			}
			companions = append(companions, sel)
		}
		in.Trash = append(in.Trash, workflow.TrashStageItem{
			Path:       pathSel,
			Companions: companions,
		})
	}
	for i, ex := range req.Extras {
		if ex.Source == "" {
			writeError(w, &validationError{msg: fmt.Sprintf("extras[%d].source is required (an inbox: selector — see kura_inbox_list)", i)})
			return
		}
		sourceSel, perr := selector.ParseInbox(ex.Source)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("extras[%d].source: %v", i, perr)})
			return
		}
		in.Extras = append(in.Extras, workflow.ExtraStageItem{
			Season: ex.Season,
			Source: sourceSel,
			Prefix: ex.Prefix,
		})
	}
	job := workflow.Stage(r.Context(), s.deps.Workflow, in)
	writeJobAck(w, job.ID(), string(jobs.KindStage), job.StartedAt())
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
