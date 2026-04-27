package progress

import "context"

type Status string

const (
	StartStatus   Status = "start"
	UpdateStatus  Status = "update"
	SuccessStatus Status = "success"
	FailureStatus Status = "failure"
)

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

func Report(ctx context.Context, event Event) {
	reporter, ok := ctx.Value(progressReporterKey{}).(Reporter)
	if !ok || reporter == nil {
		return
	}
	reporter(ctx, event)
}

func Start(ctx context.Context, stage, message string, total int) {
	Report(ctx, Event{Status: StartStatus, Stage: stage, Message: message, Total: total})
}

func Update(ctx context.Context, stage, message string, current, total int) {
	Report(ctx, Event{Status: UpdateStatus, Stage: stage, Message: message, Current: current, Total: total})
}

func Success(ctx context.Context, stage, message string, total int) {
	Report(ctx, Event{Status: SuccessStatus, Stage: stage, Message: message, Total: total})
}

func Failure(ctx context.Context, stage, message string, current, total int) {
	Report(ctx, Event{Status: FailureStatus, Stage: stage, Message: message, Current: current, Total: total})
}
