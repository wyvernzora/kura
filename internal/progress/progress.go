package progress

import "context"

type Status string

const (
	StartStatus   Status = "start"
	UpdateStatus  Status = "update"
	SuccessStatus Status = "success"
	FailureStatus Status = "failure"
)

// TotalIndeterminate is the sentinel value emitters use for an
// iterating operation whose total count is genuinely unknown ahead
// of time (or only knowable after the loop ends — e.g. "scan trash
// for the whole library", where the entry count per series is found
// during the walk). Renderers display it as a `?` so the operator
// sees `[42/?]` rather than no progress numbers at all.
//
// Distinguishes from the default total=0, which means "this is a
// one-shot op, don't show progress numbers" (used by Resolve, Add,
// and similar single-step workflows).
const TotalIndeterminate = -1

type Event struct {
	Status  Status
	Stage   string
	Message string
	Current int
	Total   int
}

type Reporter func(context.Context, Event)

type progressReporterKey struct{}

func With(ctx context.Context, reporter Reporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, progressReporterKey{}, reporter)
}

// From returns the reporter installed on ctx (or nil). Used by code
// that needs to chain its own reporter over the caller's, e.g. the
// jobs registry teeing capture-for-polling alongside the CLI spinner.
func From(ctx context.Context) Reporter {
	if r, ok := ctx.Value(progressReporterKey{}).(Reporter); ok {
		return r
	}
	return nil
}

func Report(ctx context.Context, event Event) {
	reporter, ok := ctx.Value(progressReporterKey{}).(Reporter)
	if !ok || reporter == nil {
		return
	}
	reporter(ctx, event)
}

func Start(ctx context.Context, stage string, message string, total int) {
	Report(ctx, Event{Status: StartStatus, Stage: stage, Message: message, Total: total})
}

func Update(ctx context.Context, stage string, message string, current int, total int) {
	Report(ctx, Event{Status: UpdateStatus, Stage: stage, Message: message, Current: current, Total: total})
}

func Success(ctx context.Context, stage string, message string, total int) {
	Report(ctx, Event{Status: SuccessStatus, Stage: stage, Message: message, Total: total})
}

func Failure(ctx context.Context, stage string, message string, current int, total int) {
	Report(ctx, Event{Status: FailureStatus, Stage: stage, Message: message, Current: current, Total: total})
}

// Capture installs a recording reporter on ctx and returns a derived
// ctx + a pointer to the recorded events slice. Test seam: callers
// assert the events fired by a workflow without writing the
// progress.With closure boilerplate. The returned slice is appended
// to from the goroutine that emits the event; callers that read it
// concurrently with active emission must take their own lock.
func Capture(ctx context.Context) (context.Context, *[]Event) {
	events := &[]Event{}
	return With(ctx, func(_ context.Context, ev Event) {
		*events = append(*events, ev)
	}), events
}
