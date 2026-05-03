package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
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

	// resultJSON is populated on terminal-success by the goroutine
	// that runs the workflow. UntypedJob.Result() reads it for
	// polling clients.
	resultJSON json.RawMessage

	doneCh chan struct{}

	// typedJob holds the original *Job[T] so same-kind dedupe can
	// hand it back to a second caller via type assertion.
	typedJob any
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
//     the per-job timeout (KURA_JOB_TIMEOUT) and a progress tee that
//     mirrors events into the entry while forwarding to whatever
//     reporter the caller's ctx had installed.
//   - Existing job for the same series with the same kind: the
//     existing typed *Job[T] is returned (de-dupe). No new goroutine.
//   - Existing job for the same series with a different kind: a
//     pre-resolved Failed *Job[T] carrying *JobBusyError is returned.
//     No registration, no goroutine.
//
// Submit returns within milliseconds in all three branches.
func Submit[T any](
	r *Registry,
	kind JobKind,
	series refs.Series,
	fn func(ctx context.Context) (T, error),
) *Job[T] {
	parentReporter := progress.From(r.parentCtx)

	r.mu.Lock()
	if existing, ok := r.bySeries[series]; ok {
		existingKind := existing.kind
		if existingKind == kind {
			typed, ok := existing.typedJob.(*Job[T])
			r.mu.Unlock()
			if !ok {
				// Programmer error: same kind submitted with mismatched T.
				// Should never happen if KindX↔T binding is honored.
				return Failed[T](&typeMismatchError{kind: kind})
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
		return Failed[T](&JobBusyError{Series: series, Existing: busyHandle})
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
	}
	r.byID[id] = e
	r.bySeries[series] = e
	r.wg.Add(1)
	r.mu.Unlock()

	go runJob(r, j, e, fn, parentReporter)
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
	parentReporter progress.Reporter,
) {
	defer r.wg.Done()

	jobCtx, cancelJob := r.deriveJobCtx()
	defer cancelJob()

	tee := teeReporter(parentReporter, func(ev progress.Event) {
		e.mu.Lock()
		copyEv := ev
		e.progress = &copyEv
		e.mu.Unlock()

		j.mu.Lock()
		jcopy := ev
		j.progress = &jcopy
		j.mu.Unlock()
	})
	jobCtx = progress.With(jobCtx, tee)

	result, runErr := safeRun(jobCtx, fn)

	endedAt := time.Now()
	terminalErr := classifyTerminalError(jobCtx, runErr, j.id, j.kind, endedAt.Sub(j.startedAt))
	state := StatusSucceeded
	if terminalErr != nil {
		state = StatusFailed
	}

	var resultJSON json.RawMessage
	if state == StatusSucceeded {
		encoded, encErr := json.Marshal(result)
		if encErr != nil {
			// Marshal failure: treat as workflow failure with the
			// marshal error. Don't lose the goroutine.
			state = StatusFailed
			terminalErr = &resultEncodeError{Inner: encErr}
			r.log.Error("job result marshal failed", "id", j.id, "kind", j.kind, "err", encErr)
		} else {
			resultJSON = encoded
		}
	}

	// Order: typed Job state first, then entry state, then close
	// doneCh. Close-after-state ensures Wait readers see populated
	// fields. Both mutexes protect their own copies of the fields.
	j.mu.Lock()
	j.state = state
	if state == StatusSucceeded {
		j.result = result
	}
	j.err = terminalErr
	j.endedAt = endedAt
	j.mu.Unlock()

	e.mu.Lock()
	e.state = state
	e.err = terminalErr
	e.endedAt = endedAt
	e.resultJSON = resultJSON
	e.mu.Unlock()

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
// with KURA_JOB_TIMEOUT applied if configured.
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
// workflow doesn't crash the server.
func safeRun[T any](ctx context.Context, fn func(ctx context.Context) (T, error)) (result T, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = &workflowPanicError{Recovered: rec}
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
			return &JobTimeoutError{JobID: id, Kind: kind, Elapsed: elapsed}
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
// a terminal failure rather than crashing the goroutine.
type workflowPanicError struct {
	Recovered any
}

func (e *workflowPanicError) Error() string {
	return "workflow panicked: " + sprintAny(e.Recovered)
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

// generateID returns a 16-char lowercase-hex ID derived from
// crypto/rand.
func generateID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Crypto rand failure is fatal in any reasonable runtime.
		panic("jobs: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf[:])
}

// teeReporter returns a progress.Reporter that writes each event to
// capture and then forwards to parent (if non-nil). Caller invariants:
// capture must not block; parent may be nil.
func teeReporter(parent progress.Reporter, capture func(progress.Event)) progress.Reporter {
	return func(ctx context.Context, e progress.Event) {
		capture(e)
		if parent != nil {
			parent(ctx, e)
		}
	}
}
