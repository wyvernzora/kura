package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
)

func TestSubmitTerminalEntryDoesNotReserveSeries(t *testing.T) {
	series, err := refs.ParseSeries("terminal-series")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []Status{StatusSucceeded, StatusFailed} {
		for _, kind := range []JobKind{KindScan, KindReconcileApply} {
			t.Run(state.String()+"/"+string(kind), func(t *testing.T) {
				r := NewRegistry(t.Context(), Config{}, nil)
				t.Cleanup(func() { r.Shutdown(time.Second) })

				// Polling can observe terminal state while runJob is still
				// persisting history, before it removes the series reservation.
				old := &Job[int]{
					id: "previous", kind: KindScan, series: series,
					tracked: true, state: state, doneCh: make(chan struct{}),
				}
				e := &entry{
					id: old.id, kind: old.kind, series: series,
					state: state, typedJob: old, doneCh: old.doneCh,
				}
				r.byID[e.id] = e
				r.bySeries[series] = e

				next := Submit(r, t.Context(), kind, series, func(context.Context) (int, error) {
					return 42, nil
				})
				if next == old || !next.IsTracked() || next.ID() == "" || next.ID() == old.ID() {
					t.Fatalf("Submit after %s returned stale or untracked job: id=%q tracked=%v", state, next.ID(), next.IsTracked())
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				value, err := next.Wait(ctx)
				if err != nil || value != 42 {
					t.Fatalf("new job result = (%d, %v), want (42, nil)", value, err)
				}
			})
		}
	}
}
