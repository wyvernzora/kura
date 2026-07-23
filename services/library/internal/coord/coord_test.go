package coord

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
)

func mustRef(t *testing.T, name string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries(%q): %v", name, err)
	}
	return ref
}

func TestCLICoordinator_NoOp(t *testing.T) {
	c := NewCLICoordinator()
	called := false
	if err := c.WithSeries(context.Background(), mustRef(t, "Show A"), func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("WithSeries: %v", err)
	}
	if !called {
		t.Fatal("fn was not invoked")
	}
}

func TestMCPCoordinator_SerializesSameSeries(t *testing.T) {
	c := NewMCPCoordinator()
	ref := mustRef(t, "Show A")
	var counter atomic.Int32
	var maxConcurrent atomic.Int32
	var wg sync.WaitGroup

	for range 10 {
		wg.Go(func() {
			_ = c.WithSeries(context.Background(), ref, func() error {
				current := counter.Add(1)
				for {
					prev := maxConcurrent.Load()
					if current <= prev || maxConcurrent.CompareAndSwap(prev, current) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				counter.Add(-1)
				return nil
			})
		})
	}
	wg.Wait()

	if got := maxConcurrent.Load(); got != 1 {
		t.Fatalf("max concurrent = %d, want 1", got)
	}
}

func TestMCPCoordinator_DifferentSeriesRunInParallel(t *testing.T) {
	c := NewMCPCoordinator()
	a := mustRef(t, "Show A")
	b := mustRef(t, "Show B")

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup

	for _, ref := range []refs.Series{a, b} {
		wg.Go(func() {
			_ = c.WithSeries(context.Background(), ref, func() error {
				started <- struct{}{}
				<-release
				return nil
			})
		})
	}

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("two different-series goroutines did not run in parallel")
		}
	}
	close(release)
	wg.Wait()
}

func TestRetryOnConflict_StopsOnNonConflictError(t *testing.T) {
	calls := 0
	otherErr := errors.New("not a conflict")
	err := RetryOnConflict(3, func() error {
		calls++
		return otherErr
	})
	if !errors.Is(err, otherErr) {
		t.Fatalf("err = %v, want otherErr", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRetryOnConflict_ExhaustsThenSurfaces(t *testing.T) {
	calls := 0
	conflict := &ConflictError{Scope: "series:foo", Phase: "pre_write"}
	err := RetryOnConflict(3, func() error {
		calls++
		return conflict
	})
	var got *ConflictError
	if !errors.As(err, &got) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryOnConflict_SucceedsOnRetry(t *testing.T) {
	calls := 0
	err := RetryOnConflict(3, func() error {
		calls++
		if calls == 1 {
			return &ConflictError{Scope: "series:foo", Phase: "pre_write"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestRetryOnConflict_ZeroAttemptsBecomesOne(t *testing.T) {
	calls := 0
	_ = RetryOnConflict(0, func() error {
		calls++
		return nil
	})
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestMCPCoordinator_RetryHappensInsideMutex(t *testing.T) {
	// Composition contract: callers wrap RetryOnConflict inside the
	// WithSeries closure so retry attempts stay serialized under the
	// mutex. Reusing this pattern across N goroutines must keep
	// max-in-flight at 1.
	c := NewMCPCoordinator()
	ref := mustRef(t, "Show A")
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var wg sync.WaitGroup

	for range 5 {
		wg.Go(func() {
			attempts := 0
			_ = c.WithSeries(context.Background(), ref, func() error {
				return RetryOnConflict(3, func() error {
					current := inFlight.Add(1)
					for {
						prev := maxInFlight.Load()
						if current <= prev || maxInFlight.CompareAndSwap(prev, current) {
							break
						}
					}
					time.Sleep(2 * time.Millisecond)
					inFlight.Add(-1)
					attempts++
					if attempts == 1 {
						return &ConflictError{Scope: "series:foo", Phase: "pre_write"}
					}
					return nil
				})
			})
		})
	}
	wg.Wait()

	if got := maxInFlight.Load(); got != 1 {
		t.Fatalf("max in-flight = %d, want 1 (retry must stay inside the mutex)", got)
	}
}

func TestMCPCoordinator_WithSeries_HonoursCtxCancel(t *testing.T) {
	c := NewMCPCoordinator()
	ref := mustRef(t, "Show A")
	ready := make(chan struct{})
	holderRunning := make(chan struct{})
	holderDone := make(chan struct{})

	// Goroutine 1 acquires the per-series semaphore and blocks.
	go func() {
		_ = c.WithSeries(context.Background(), ref, func() error {
			close(ready)
			<-holderRunning
			return nil
		})
		close(holderDone)
	}()

	<-ready
	// Goroutine 2 attempts the same series with a cancelled ctx; must
	// return ctx.Err() without invoking fn.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fnInvoked := false
	err := c.WithSeries(ctx, ref, func() error {
		fnInvoked = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if fnInvoked {
		t.Fatal("fn must not run when ctx already cancelled")
	}

	// Release goroutine 1.
	close(holderRunning)
	<-holderDone
}

func TestCLICoordinator_RespectsCancelledCtx(t *testing.T) {
	c := NewCLICoordinator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := c.WithSeries(ctx, mustRef(t, "Show A"), func() error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("fn must not run on cancelled ctx")
	}
}

func TestIsStaleHolder_DifferentHostNeverStale(t *testing.T) {
	h := Holder{
		PID:  os.Getpid(),
		Host: "definitely-not-this-host-" + strings.Repeat("x", 16),
	}
	if IsStaleHolder(h) {
		t.Fatal("cross-host claim reported as stale; cross-host claims must never auto-break")
	}
}

func TestIsStaleHolder_LiveProcessNotStale(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("staleness detection is Unix-only")
	}
	h := Holder{
		PID:  os.Getpid(),
		Host: currentHost(),
	}
	if IsStaleHolder(h) {
		t.Fatal("our own live process reported as stale")
	}
}

func TestIsStaleHolder_DeadProcessIsStale(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("staleness detection is Unix-only")
	}
	// Spawn a subprocess and wait for it to exit. On most systems the
	// PID won't be reused immediately and signal(0) returns ESRCH.
	// Some kernels keep the entry in the process table briefly after
	// exit; retry a few times before giving up.
	deadPID := findDeadPID(t)
	h := Holder{PID: deadPID, Host: currentHost()}
	if !IsStaleHolder(h) {
		t.Fatalf("dead pid %d not reported as stale", deadPID)
	}
}

func findDeadPID(t *testing.T) int {
	t.Helper()
	for range 5 {
		cmd := exec.Command("/bin/sh", "-c", "exit 0")
		if err := cmd.Run(); err != nil {
			t.Fatalf("seed subprocess: %v", err)
		}
		// Brief pause to let the kernel finalize PID cleanup on
		// systems that defer it.
		time.Sleep(20 * time.Millisecond)
		if processIsDefinitelyDead(cmd.Process.Pid) {
			return cmd.Process.Pid
		}
	}
	t.Skip("could not find a definitely-dead PID after 5 attempts; flaky on this kernel")
	return 0
}

func TestIsStaleHolder_InvalidPIDIsStale(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		t.Skip("staleness detection is Unix-only")
	}
	h := Holder{PID: 0, Host: currentHost()}
	if !IsStaleHolder(h) {
		t.Fatal("pid 0 not reported as stale")
	}
}

func TestCurrentHost_KuraHostIDOverride(t *testing.T) {
	// Save and clear the test seam so the env-var path is exercised.
	prevOverride := hostnameOverride
	hostnameOverride = ""
	defer func() { hostnameOverride = prevOverride }()

	t.Setenv("KURA_HOST_ID", "kura-stable")
	if got := currentHost(); got != "kura-stable" {
		t.Fatalf("currentHost() = %q, want kura-stable", got)
	}

	// Test seam still wins when both are set.
	hostnameOverride = "test-host"
	if got := currentHost(); got != "test-host" {
		t.Fatalf("with override set, currentHost() = %q, want test-host", got)
	}
}

func TestNewHolderAndMutator_PopulateFields(t *testing.T) {
	original := nowFunc
	defer func() { nowFunc = original }()
	fixed := time.Date(2026, 5, 2, 19, 14, 33, 0, time.UTC)
	nowFunc = func() time.Time { return fixed }

	h := NewHolder("reconcile_apply", "abc123")
	if h.Op != "reconcile_apply" || h.Token != "abc123" {
		t.Fatalf("Holder = %+v", h)
	}
	if h.PID != os.Getpid() {
		t.Fatalf("PID = %d, want %d", h.PID, os.Getpid())
	}
	if !h.Started.Equal(fixed) {
		t.Fatalf("Started = %v, want %v", h.Started, fixed)
	}

	m := NewMutator("stage")
	if m.Op != "stage" || m.PID != os.Getpid() {
		t.Fatalf("Mutator = %+v", m)
	}
	if !m.At.Equal(fixed) {
		t.Fatalf("At = %v, want %v", m.At, fixed)
	}
}

func TestBusyError_Format(t *testing.T) {
	original := nowFunc
	defer func() { nowFunc = original }()
	nowFunc = func() time.Time { return time.Date(2026, 5, 2, 19, 14, 0, 0, time.UTC) }

	holder := Holder{
		Op:      "reconcile_apply",
		PID:     12345,
		Host:    "workstation",
		Started: time.Date(2026, 5, 2, 19, 10, 0, 0, time.UTC),
	}
	err := &BusyError{Scope: "series:tvdb:370070", Holder: holder}
	got := err.Error()
	if !strings.Contains(got, "tvdb:370070") || !strings.Contains(got, "reconcile_apply") || !strings.Contains(got, "workstation") || !strings.Contains(got, "12345") {
		t.Fatalf("BusyError format missing fields: %q", got)
	}
}

func TestConflictError_FormatIncludesMutator(t *testing.T) {
	mutator := Mutator{
		Op:   "stage",
		PID:  999,
		Host: "workstation",
		At:   time.Date(2026, 5, 2, 19, 14, 30, 0, time.UTC),
	}
	err := &ConflictError{Scope: "series:tvdb:370070", Phase: "pre_write", Mutator: mutator}
	got := err.Error()
	if !strings.Contains(got, "lost race to stage") || !strings.Contains(got, "pid=999") {
		t.Fatalf("ConflictError format = %q", got)
	}
}

func TestConflictError_FormatWithoutMutator(t *testing.T) {
	err := &ConflictError{Scope: "series:tvdb:370070", Phase: "post_write"}
	got := err.Error()
	if !strings.Contains(got, "post_write") || strings.Contains(got, "lost race") {
		t.Fatalf("ConflictError no-mutator format = %q", got)
	}
}
