package jobs

import (
	"context"
	"encoding/json"
	"runtime/debug"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/progress"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/jobfile"
)

// Logger is the small interface the registry uses for lifecycle and
// error logs. Designed to plug into either stdlib log or a structured
// logger.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// nopLogger drops everything; default when no logger is supplied.
type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// Config controls registry timing. Zero values disable the
// corresponding loop or behavior (test seam).
type Config struct {
	JobTimeout     time.Duration // 0 disables per-job deadline
	Retention      time.Duration // 0 disables reaper eviction (test only)
	ReaperInterval time.Duration // 0 disables reaper goroutine (test only)
	// LibRoot is the library root used to persist per-job JSONL logs
	// at <libRoot>/.kura/jobs/<id>.jsonl. Empty disables persistence
	// — preserves CLI/test paths that construct registries without a
	// library root.
	LibRoot string
}

// Registry tracks in-flight and recently-terminal long jobs. One per
// process (CLI invocation or kura serve lifetime). Shared across
// transports under kura serve.
type Registry struct {
	parentCtx context.Context
	cancel    context.CancelFunc

	mu       sync.RWMutex
	byID     map[string]*entry
	bySeries map[refs.Series]*entry

	wg sync.WaitGroup // tracks live job goroutines for Shutdown

	cfg Config
	log Logger
}

// entry is the type-erased per-job record stored in the registry's
// indexes. The typed *Job[T] view is held alongside via typedJob and
// returned to callers from Submit's same-kind dedupe path.
type entry struct {
	id        string
	kind      JobKind
	series    refs.Series
	startedAt time.Time

	mu sync.RWMutex

	state    Status
	err      error
	endedAt  time.Time
	progress *progress.Event

	// resultJSON is populated for terminal jobs when the workflow
	// result can be marshalled. UntypedJob.Result() exposes it only on
	// success; UntypedJob.TerminalResult() exposes it for transports
	// that need partial failure detail.
	resultJSON json.RawMessage

	doneCh chan struct{}

	// typedJob holds the original *Job[T] so same-kind dedupe can
	// hand it back to a second caller via type assertion.
	typedJob any

	// callerCtx is the ctx the submitter passed to Submit. runJob
	// reads progress.From(callerCtx) to honor a per-call reporter
	// (CLI spinner, future HTTP request scope). The job's cancellation
	// lifecycle is independent and bound to r.parentCtx.
	callerCtx context.Context
}

// NewRegistry constructs a Registry whose lifetime is bounded by
// parentCtx. The reaper is not started by this constructor (added in
// commit 3). Call Shutdown(grace) to cancel in-flight jobs and wait
// for them to finish.
func NewRegistry(parentCtx context.Context, cfg Config, log Logger) *Registry {
	if log == nil {
		log = nopLogger{}
	}
	ctx, cancel := context.WithCancel(parentCtx)
	r := &Registry{
		parentCtx: ctx,
		cancel:    cancel,
		byID:      map[string]*entry{},
		bySeries:  map[refs.Series]*entry{},
		cfg:       cfg,
		log:       log,
	}
	r.startReaper()
	return r
}

// Shutdown cancels the registry's parent ctx (which propagates to all
// in-flight job goroutines via their derived ctx) and waits up to
// grace for them to finish. Returns the count of goroutines still
// running after grace expires; callers can log it before exiting.
func (r *Registry) Shutdown(grace time.Duration) int {
	r.cancel()
	if grace <= 0 {
		// Don't wait; leak best-effort. Used by short-lived CLI.
		return 0
	}
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return 0
	case <-time.After(grace):
		r.mu.RLock()
		stuck := 0
		for _, e := range r.byID {
			e.mu.RLock()
			if e.state == StatusRunning {
				stuck++
			}
			e.mu.RUnlock()
		}
		r.mu.RUnlock()
		return stuck
	}
}

// Submit registers a tracked job for (kind, series) and spawns a
// goroutine to run fn. Three outcomes:
//
//   - No existing job for series: a fresh tracked Job is created and
//     returned. The goroutine runs fn with a derived ctx that carries
//     the configured per-job timeout and a capture-only
//     progress reporter that records the latest event into the job
//     and entry. Consumers (CLI, MCP, REST) poll for progress via
//     Job.LatestProgress / UntypedJob.Progress; the registry does
//     NOT forward events to whatever reporter the caller's ctx had
//     installed.
//   - Existing job for the same series with the same kind: the
//     existing typed *Job[T] is returned (de-dupe). No new goroutine.
//   - Existing job for the same series with a different kind: an untracked
//     failed *Job[T] carrying *JobBusyError is returned.
//     No registration, no goroutine.
//
// Submit returns within milliseconds in all three branches. Errors
// surface asynchronously: the typed Job holds them in state. Callers
// must check via Job.Wait (CLI) or by polling Job.State / UntypedJob
// (MCP). In particular, if the registry's parent ctx is already
// cancelled when Submit is called, the goroutine starts and
// immediately reaches terminal-failed with errShutdown — Submit
// itself returns the live tracked Job, not an error.
//
// callerCtx is the submitter's ctx. runJob reads progress.From on it
// to discover any per-call reporter (e.g. the CLI spinner installed on
// rt.Context, or a per-request reporter from a future HTTP transport).
// callerCtx is NOT used for cancellation — the job's lifecycle stays
// bound to the registry's parent ctx so Shutdown semantics are
// independent of the caller's request lifetime.
func Submit[T any](
	r *Registry,
	callerCtx context.Context,
	kind JobKind,
	series refs.Series,
	fn func(ctx context.Context) (T, error),
) *Job[T] {
	r.mu.Lock()
	if existing, ok := r.bySeries[series]; ok {
		existingKind := existing.kind
		if existingKind == kind {
			typed, ok := existing.typedJob.(*Job[T])
			r.mu.Unlock()
			if !ok {
				// Programmer error: same kind submitted with mismatched T.
				// Should never happen if KindX↔T binding is honored.
				return failed[T](&typeMismatchError{kind: kind})
			}
			return typed
		}
		// Cross-kind: build BusyHandle from existing entry.
		busyHandle := BusyHandle{
			JobID:     existing.id,
			Kind:      existing.kind,
			Series:    existing.series,
			StartedAt: existing.startedAt,
		}
		r.mu.Unlock()
		return failed[T](&JobBusyError{Series: series, Existing: busyHandle})
	}

	id := generateID()
	now := time.Now()
	doneCh := make(chan struct{})

	j := &Job[T]{
		id:        id,
		kind:      kind,
		series:    series,
		startedAt: now,
		tracked:   true,
		state:     StatusRunning,
		doneCh:    doneCh,
	}
	e := &entry{
		id:        id,
		kind:      kind,
		series:    series,
		startedAt: now,
		state:     StatusRunning,
		doneCh:    doneCh,
		typedJob:  j,
		callerCtx: callerCtx,
	}
	r.byID[id] = e
	r.bySeries[series] = e
	r.wg.Add(1)
	r.mu.Unlock()

	r.log.Info("job submitted", "id", id, "kind", kind, "series", series.String())
	go runJob(r, j, e, fn)
	return j
}

// runJob executes fn in the goroutine, captures terminal state, and
// updates the entry. Removes the entry from bySeries on terminal so
// future submissions can spawn.
func runJob[T any](
	r *Registry,
	j *Job[T],
	e *entry,
	fn func(ctx context.Context) (T, error),
) {
	defer r.wg.Done()

	jobCtx, cancelJob := r.deriveJobCtx()
	defer cancelJob()

	// Open the persistent JSONL writer when the registry has a
	// LibRoot. Persistence is opportunistic: if Create fails, log
	// and proceed with an in-memory-only run.
	writer := openJobWriter(r, j)
	defer func() {
		if writer != nil {
			_ = writer.Close()
		}
	}()

	// Capture + relay reporter. Captures the latest event onto the
	// entry (UntypedJob.Progress polling) and the typed Job
	// (Job.LatestProgress polling) so async consumers can poll, AND
	// forwards to the caller's reporter when one is installed. The
	// caller's ctx (passed to Submit) is the source — the CLI installs
	// its spinner reporter on rt.Context, MCP installs none, future
	// per-request transports install per-call reporters. Reading from
	// the caller's ctx (not the registry-derived jobCtx) lets each
	// submission honor its own reporter without depending on registry
	// construction having seen the user-facing parent.
	parentReporter := progress.From(e.callerCtx)
	jobCtx = progress.With(jobCtx, func(ctx context.Context, ev progress.Event) {
		e.mu.Lock()
		entryCopy := ev
		e.progress = &entryCopy
		e.mu.Unlock()

		j.mu.Lock()
		jobCopy := ev
		j.progress = &jobCopy
		j.mu.Unlock()

		if writer != nil {
			if err := writer.AppendProgress(jobfile.ProgressLine{
				At:      time.Now().UTC().Format(time.RFC3339Nano),
				Stage:   ev.Stage,
				Status:  string(ev.Status),
				Current: ev.Current,
				Total:   ev.Total,
				Message: ev.Message,
			}); err != nil {
				r.log.Warn("jobfile append progress failed", "id", j.id, "err", err)
			}
		}

		if parentReporter != nil {
			parentReporter(ctx, ev)
		}
	})

	result, runErr := safeRun(jobCtx, fn)

	endedAt := time.Now()
	terminalErr := classifyTerminalError(jobCtx, runErr, j.id, j.kind, endedAt.Sub(j.startedAt))
	state := StatusSucceeded
	if terminalErr != nil {
		state = StatusFailed
	}

	encoded, encErr := json.Marshal(result)
	var resultJSON json.RawMessage
	if encErr != nil {
		if state == StatusSucceeded {
			// Marshal failure: treat as workflow failure with the
			// marshal error. Don't lose the goroutine.
			state = StatusFailed
			terminalErr = &resultEncodeError{Inner: encErr}
			r.log.Error("job result marshal failed", "id", j.id, "kind", j.kind, "err", encErr)
		} else {
			r.log.Warn("failed job result marshal failed", "id", j.id, "kind", j.kind, "err", encErr)
		}
	} else {
		resultJSON = encoded
	}

	// Order: typed Job state first, then entry state, then close
	// doneCh. Close-after-state ensures Wait readers see populated
	// fields. Both mutexes protect their own copies of the fields.
	j.mu.Lock()
	j.state = state
	j.result = result
	j.err = terminalErr
	j.endedAt = endedAt
	j.mu.Unlock()

	e.mu.Lock()
	e.state = state
	e.err = terminalErr
	e.endedAt = endedAt
	e.resultJSON = resultJSON
	e.mu.Unlock()

	if writer != nil {
		if err := writer.AppendTerminal(buildTerminalLine(state, terminalErr, resultJSON, endedAt)); err != nil {
			r.log.Warn("jobfile append terminal failed", "id", j.id, "err", err)
		}
	}

	// Remove from bySeries so the next submission can spawn.
	r.mu.Lock()
	if cur, ok := r.bySeries[e.series]; ok && cur == e {
		delete(r.bySeries, e.series)
	}
	r.mu.Unlock()

	close(doneChOf(j))
	r.log.Info("job terminal", "id", j.id, "kind", j.kind, "series", j.series, "state", state.String())
}

// deriveJobCtx returns a ctx derived from the registry's parent ctx,
// with the configured job timeout applied.
func (r *Registry) deriveJobCtx() (context.Context, context.CancelFunc) {
	if r.cfg.JobTimeout <= 0 {
		ctx, cancel := context.WithCancel(r.parentCtx)
		return ctx, cancel
	}
	return context.WithTimeout(r.parentCtx, r.cfg.JobTimeout)
}

// doneChOf accesses the unexported doneCh on Job[T] from within the
// jobs package. Lifted out for clarity; could also be a method.
func doneChOf[T any](j *Job[T]) chan struct{} {
	return j.doneCh
}

// safeRun calls fn and recovers panics into errors so a panicking
// workflow doesn't crash the server. The recovered stack trace is
// captured into the typed error so post-mortem inspection sees the
// frame that panicked, not just the recover() site.
func safeRun[T any](ctx context.Context, fn func(ctx context.Context) (T, error)) (result T, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = &workflowPanicError{Recovered: rec, Stack: debug.Stack()}
		}
	}()
	return fn(ctx)
}

// classifyTerminalError maps the goroutine's return error to the
// terminal error stored on the job. Distinguishes timeout (deadline
// exceeded on derived ctx), shutdown (parent ctx cancelled), and
// arbitrary workflow errors.
func classifyTerminalError(jobCtx context.Context, err error, id string, kind JobKind, elapsed time.Duration) error {
	if err == nil {
		return nil
	}
	// Distinguish timeout vs shutdown by inspecting ctx.Err type.
	if ctxErr := jobCtx.Err(); ctxErr != nil {
		if ctxErr == context.DeadlineExceeded {
			return &JobTimeoutError{JobID: id, JobKind: kind, Elapsed: elapsed}
		}
		// context.Canceled: parent ctx cancelled, e.g. shutdown.
		// Return a sentinel so the error mapper can render kind="shutdown".
		return errShutdown{JobID: id}
	}
	return err
}

// resultEncodeError signals the goroutine returned a value that
// json.Marshal could not encode. Surfaces as a terminal failure so
// the goroutine doesn't leak.
type resultEncodeError struct {
	Inner error
}

func (e *resultEncodeError) Error() string {
	return "result encode failed: " + e.Inner.Error()
}

func (e *resultEncodeError) Unwrap() error { return e.Inner }

// errShutdown signals a job terminated because the registry's parent
// ctx was cancelled (server SIGTERM / CLI Ctrl-C cancelling the
// registry parent). Distinct from JobTimeoutError so the mapping
// table can surface kind="shutdown".
type errShutdown struct {
	JobID string
}

func (e errShutdown) Error() string {
	return "job " + e.JobID + " terminated by shutdown"
}

// workflowPanicError wraps a recovered panic so it can be surfaced as
// a terminal failure rather than crashing the goroutine. Stack carries
// the runtime/debug.Stack() snapshot taken at recover time so the
// originating frame is preserved for post-mortem inspection.
type workflowPanicError struct {
	Recovered any
	Stack     []byte
}

func (e *workflowPanicError) Error() string {
	msg := "workflow panicked: " + sprintAny(e.Recovered)
	if len(e.Stack) > 0 {
		msg += "\n" + string(e.Stack)
	}
	return msg
}

func sprintAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return "unknown panic"
}

// typeMismatchError indicates same-kind dedupe found an entry whose
// typedJob doesn't satisfy *Job[T] for the requested T. Programmer
// error if the KindX↔T convention is honored.
type typeMismatchError struct {
	kind JobKind
}

func (e *typeMismatchError) Error() string {
	return "internal: type mismatch on dedupe for kind " + string(e.kind)
}

// generateID returns a 26-char Crockford-base32 ULID. The leading
// 48 bits encode a millisecond timestamp, so plain `ls` of
// `<library>/.kura/jobs/` is naturally chronological. Same scheme
// kura already uses for trash buckets.
func generateID() string {
	return ulid.Make().String()
}
