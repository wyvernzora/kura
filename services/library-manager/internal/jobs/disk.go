package jobs

import (
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/errkind"
	"github.com/wyvernzora/kura/services/library-manager/internal/progress"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/jobfile"
)

// openJobWriter opens the persistent JSONL writer for a tracked job
// when the registry has a LibRoot configured. Returns nil (no
// persistence) when the LibRoot is empty or the open fails — the
// per-job goroutine logs and continues.
func openJobWriter[T any](r *Registry, j *Job[T]) *jobfile.Writer {
	if r.cfg.LibRoot == "" {
		return nil
	}
	header := jobfile.HeaderLine{
		JobID:     j.id,
		Kind:      string(j.kind),
		SeriesRef: j.series.String(),
		StartedAt: j.startedAt.UTC().Format(time.RFC3339Nano),
	}
	w, err := jobfile.Create(r.cfg.LibRoot, header)
	if err != nil {
		r.log.Warn("jobfile create failed", "id", j.id, "kind", j.kind, "err", err)
		return nil
	}
	return w
}

// buildTerminalLine projects (state, terminalErr, resultJSON) into the
// JSONL terminal record. Mirrors the errkind path the REST/MCP error
// envelope already speaks.
func buildTerminalLine(state Status, terminalErr error, resultJSON json.RawMessage, endedAt time.Time) jobfile.TerminalLine {
	line := jobfile.TerminalLine{
		At:    endedAt.UTC().Format(time.RFC3339Nano),
		State: state.String(),
	}
	if len(resultJSON) > 0 {
		line.Result = resultJSON
	}
	if state == StatusSucceeded {
		return line
	}
	// Failed — project the typed error if it satisfies errkind.Coded;
	// otherwise stamp KindInternal.
	envelope := &jobfile.TerminalError{
		Kind:    errkind.KindInternal,
		Message: "",
	}
	if terminalErr != nil {
		envelope.Message = terminalErr.Error()
		if coded, ok := errors.AsType[errkind.Coded](terminalErr); ok {
			envelope.Kind = coded.Kind()
			envelope.Data = coded.Data()
		}
	}
	line.Error = envelope
	return line
}

// ActiveIDs snapshots the current set of in-flight job IDs. Used by
// the sweep to skip files that are still being written.
func (r *Registry) ActiveIDs() map[string]struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]struct{}, len(r.byID))
	for id, e := range r.byID {
		e.mu.RLock()
		running := e.state == StatusRunning
		e.mu.RUnlock()
		if running {
			out[id] = struct{}{}
		}
	}
	return out
}

// diskJob adapts a parsed jobfile.Job to the UntypedJob interface so
// Registry.Get can return persisted jobs after the in-memory entry
// has been evicted.
type diskJob struct {
	id        string
	kind      string
	series    refs.Series
	startedAt time.Time
	endedAt   time.Time
	hasEnded  bool
	state     Status
	progress  *progress.Event
	result    json.RawMessage
	err       error
}

func (d *diskJob) ID() string                 { return d.id }
func (d *diskJob) Kind() string               { return d.kind }
func (d *diskJob) Series() refs.Series        { return d.series }
func (d *diskJob) StartedAt() time.Time       { return d.startedAt }
func (d *diskJob) State() Status              { return d.state }
func (d *diskJob) EndedAt() (time.Time, bool) { return d.endedAt, d.hasEnded }
func (d *diskJob) Progress() *progress.Event {
	if d.progress == nil {
		return nil
	}
	cp := *d.progress
	return &cp
}
func (d *diskJob) Result() json.RawMessage {
	if d.state != StatusSucceeded || len(d.result) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(d.result))
	copy(out, d.result)
	return out
}

func (d *diskJob) TerminalResult() json.RawMessage {
	if d.state == StatusRunning || len(d.result) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(d.result))
	copy(out, d.result)
	return out
}

func (d *diskJob) Err() error { return d.err }

// projectFromDisk maps a parsed jobfile.Job into the UntypedJob view.
// Missing terminal lines synthesize state=failed with the shutdown
// sentinel — the writer goroutine is gone, so the job will never
// emit one again.
func projectFromDisk(job jobfile.Job) UntypedJob {
	d := &diskJob{
		id:   job.Header.JobID,
		kind: job.Header.Kind,
	}
	if job.Header.SeriesRef != "" {
		if ref, err := refs.ParseSeries(job.Header.SeriesRef); err == nil {
			d.series = ref
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, job.Header.StartedAt); err == nil {
		d.startedAt = t
	}
	if len(job.Progress) > 0 {
		last := job.Progress[len(job.Progress)-1]
		ev := progress.Event{
			Status:  progress.Status(last.Status),
			Stage:   last.Stage,
			Message: last.Message,
			Current: last.Current,
			Total:   last.Total,
		}
		d.progress = &ev
	}
	if job.Terminal != nil {
		d.hasEnded = true
		if t, err := time.Parse(time.RFC3339Nano, job.Terminal.At); err == nil {
			d.endedAt = t
		}
		switch job.Terminal.State {
		case "succeeded":
			d.state = StatusSucceeded
			d.result = job.Terminal.Result
		default:
			d.state = StatusFailed
			d.result = job.Terminal.Result
			if job.Terminal.Error != nil {
				d.err = &reconstructedError{
					kind:    job.Terminal.Error.Kind,
					message: job.Terminal.Error.Message,
					data:    job.Terminal.Error.Data,
				}
			}
		}
		return d
	}
	// No terminal line — writer goroutine is gone. Synthesize the
	// shutdown sentinel so transports render kind="shutdown" the same
	// way they would for a live registry-evicted job.
	d.hasEnded = true
	d.endedAt = d.startedAt
	d.state = StatusFailed
	d.err = errShutdown{JobID: job.Header.JobID}
	return d
}

// reconstructedError satisfies errkind.Coded so the REST/MCP error
// envelope renders persisted failures the same way a live registry
// entry would.
type reconstructedError struct {
	kind    string
	message string
	data    map[string]any
}

func (e *reconstructedError) Error() string        { return e.message }
func (e *reconstructedError) Kind() string         { return e.kind }
func (e *reconstructedError) Category() string     { return errkind.CategoryInternalError }
func (e *reconstructedError) Data() map[string]any { return e.data }

// readDiskJob attempts to load a job from <libRoot>/.kura/jobs/<id>.jsonl.
// Returns *JobNotFoundError when the file is absent.
func readDiskJob(libRoot, id string) (UntypedJob, error) {
	job, err := jobfile.Read(libRoot, id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &JobNotFoundError{JobID: id}
		}
		return nil, err
	}
	return projectFromDisk(job), nil
}
