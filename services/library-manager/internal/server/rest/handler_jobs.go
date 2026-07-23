package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
)

const (
	// jobStreamPollInterval bounds how often the SSE stream polls the
	// registry for new state when no in-process notification channel
	// exists. The job registry doesn't broadcast events, so SSE samples
	// at this cadence and emits when state/progress changes.
	jobStreamPollInterval = 250 * time.Millisecond

	// jobStreamMaxDuration caps how long a single SSE connection can
	// hang. Server enforces an upper bound so a wedged client doesn't
	// pin a goroutine forever; clients reconnect if they want more.
	jobStreamMaxDuration = 30 * time.Minute

	rfc3339Millis = "2006-01-02T15:04:05.000Z07:00"
)

var jobIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_\-]{1,128}$`)

// jobStatus is the GET /api/v1/jobs/{job} response shape. Mirrors the
// MCP tool_job_status projection so REST and MCP describe jobs the
// same way. Per Product.md "Selectors, not paths," the response
// surfaces metadataRef (looked up via index) rather than the
// internal SeriesRef.
type jobStatus struct {
	JobID       string          `json:"jobId"`
	Kind        string          `json:"kind"`
	MetadataRef string          `json:"metadataRef,omitempty"`
	State       string          `json:"state"`
	StartedAt   string          `json:"startedAt"`
	EndedAt     string          `json:"endedAt,omitempty"`
	Progress    *jobProgress    `json:"progress,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       *jobError       `json:"error,omitempty"`
}

type jobProgress struct {
	Phase   string `json:"phase"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

type jobError struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// handleJobStatus serves GET /api/v1/jobs/{job}.
func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("job")
	if !jobIDPattern.MatchString(id) {
		writeError(w, &validationError{msg: fmt.Sprintf("invalid job ID %q", id)})
		return
	}
	view, err := s.deps.Workflow.Jobs.Get(id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.projectJobStatus(view))
}

// projectJobStatus converts a registry view into the wire shape.
// Translates the job's stored SeriesRef to the metadata ref the
// agent / WebUI sees via Index.GetRow (O(1)).
func (s *Server) projectJobStatus(view jobs.UntypedJob) jobStatus {
	var metaRef string
	if row, ok := s.deps.Workflow.Index.GetRow(view.Series()); ok {
		metaRef = row.Metadata.String()
	}
	out := jobStatus{
		JobID:       view.ID(),
		Kind:        view.Kind(),
		MetadataRef: metaRef,
		State:       view.State().String(),
		StartedAt:   view.StartedAt().UTC().Format(rfc3339Millis),
	}
	if endedAt, ok := view.EndedAt(); ok {
		out.EndedAt = endedAt.UTC().Format(rfc3339Millis)
	}
	if ev := view.Progress(); ev != nil {
		out.Progress = &jobProgress{
			Phase:   ev.Stage,
			Status:  string(ev.Status),
			Message: ev.Message,
			Current: ev.Current,
			Total:   ev.Total,
		}
	}
	if raw := view.Result(); len(raw) > 0 {
		out.Result = raw
	}
	if err := view.Err(); err != nil {
		out.Error = projectJobError(err)
	}
	return out
}

func projectJobError(err error) *jobError {
	if jobs.IsShutdownError(err) {
		return &jobError{Kind: errkind.KindInternal, Message: err.Error()}
	}
	if coded, ok := errors.AsType[errkind.Coded](err); ok {
		return &jobError{Kind: coded.Kind(), Message: err.Error(), Data: coded.Data()}
	}
	return &jobError{Kind: errkind.KindInternal, Message: err.Error()}
}

// handleJobStream serves GET /api/v1/jobs/{job}/stream.
//
// SSE events:
//
//	event: progress  - latest progress.Event when state advances
//	event: result    - terminal success
//	event: error     - terminal failure
//
// Connection closes after the first terminal event. Replays the
// terminal event when client connects after the job already finished.
func (s *Server) handleJobStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("job")
	if !jobIDPattern.MatchString(id) {
		writeError(w, &validationError{msg: fmt.Sprintf("invalid job ID %q", id)})
		return
	}
	view, err := s.deps.Workflow.Jobs.Get(id)
	if err != nil {
		writeError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, &internalError{msg: "streaming unsupported by transport"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set(headerCacheControl, cacheControlNoStore)
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	deadline := time.Now().Add(jobStreamMaxDuration)
	ctx := r.Context()

	var lastProgress *jobProgress
	var lastState string

	for {
		current := s.projectJobStatus(view)

		// progress event when the latest progress.Event changed
		if current.Progress != nil && !sameProgress(lastProgress, current.Progress) {
			writeSSE(w, "progress", current.Progress)
			flusher.Flush()
			lastProgress = current.Progress
		}

		// terminal event
		if current.State != lastState && current.State != "running" {
			switch {
			case current.Result != nil:
				writeSSERaw(w, "result", current.Result)
			case current.Error != nil:
				writeSSE(w, "error", current.Error)
			default:
				writeSSE(w, "result", nil)
			}
			flusher.Flush()
			return
		}
		lastState = current.State

		select {
		case <-ctx.Done():
			return
		case <-time.After(jobStreamPollInterval):
		}
		if time.Now().After(deadline) {
			return
		}
	}
}

func sameProgress(a, b *jobProgress) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Phase == b.Phase &&
		a.Status == b.Status &&
		a.Message == b.Message &&
		a.Current == b.Current &&
		a.Total == b.Total
}

// writeSSE writes one SSE event with a JSON-encoded data payload.
func writeSSE(w http.ResponseWriter, event string, data any) {
	buf, err := json.Marshal(data)
	if err != nil {
		buf = []byte("null")
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, buf)
}

// writeSSERaw writes one SSE event with an already-JSON-encoded payload.
func writeSSERaw(w http.ResponseWriter, event string, data json.RawMessage) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}
