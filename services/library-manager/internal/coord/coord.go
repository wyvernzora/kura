package coord

import (
	"context"
	"sync"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
)

// Coordinator serializes mutations against the same series (or the
// library index) within a single process.
//
// Two impls exist:
//
//   - NewCLICoordinator returns a no-op serializer for single-goroutine
//     contexts (tests, one-shot tooling). The ctx is consulted for a
//     fast-fail check so the contract is uniform.
//
//   - NewMCPCoordinator holds a per-key channel-semaphore (sync.Map-
//     backed) so concurrent goroutine-served requests don't race the
//     same series. kura serve uses it, and also passes WithIndex into
//     indexfile as the guard serializing index snapshot writes.
//     Acquisition is cancellable via ctx, so a queued goroutine behind
//     a long apply returns ctx.Err() on shutdown instead of blocking
//     past Shutdown(grace).
//
// Retry policy is composed at the call site: callers needing CAS
// retry wrap RetryOnConflict inside the lock closure (see the
// series-mutation workflows, e.g. internal/workflow/scan.go). The
// closure ordering keeps retry inside the mutex so peers can't sneak
// a write between attempts.
//
// Workflows accept a Coordinator from Deps and don't care which
// variant they have.
type Coordinator interface {
	// WithSeries serializes goroutines per series. Returns ctx.Err()
	// if ctx is cancelled before the lock is acquired.
	WithSeries(ctx context.Context, ref refs.Series, fn func() error) error

	// WithIndex serializes index-touching ops in-process.
	WithIndex(ctx context.Context, fn func() error) error
}

// NewCLICoordinator returns a Coordinator with no in-process
// serialization. fn is invoked immediately under WithSeries/WithIndex.
// Caller-side retry composition is unaffected.
func NewCLICoordinator() Coordinator {
	return &cliCoordinator{}
}

// NewMCPCoordinator returns a Coordinator that serializes goroutines
// per-series (and globally for the index). Required for any long-
// running consumer that handles multiple concurrent requests.
func NewMCPCoordinator() Coordinator {
	return &mcpCoordinator{
		indexMu: make(chan struct{}, 1),
	}
}

type cliCoordinator struct{}

func (c *cliCoordinator) WithSeries(ctx context.Context, _ refs.Series, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func (c *cliCoordinator) WithIndex(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

type mcpCoordinator struct {
	series  sync.Map      // map[refs.Series]chan struct{}
	indexMu chan struct{} // cap 1
}

// seriesSem returns the per-series semaphore channel, allocating one
// on first use. The Load fast path avoids the allocation that
// LoadOrStore unconditionally performs.
func (m *mcpCoordinator) seriesSem(ref refs.Series) chan struct{} {
	if v, ok := m.series.Load(ref); ok {
		return v.(chan struct{})
	}
	v, _ := m.series.LoadOrStore(ref, make(chan struct{}, 1))
	return v.(chan struct{})
}

func (m *mcpCoordinator) acquireSeries(ctx context.Context, ref refs.Series) (func(), error) {
	sem := m.seriesSem(ref)
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mcpCoordinator) acquireIndex(ctx context.Context) (func(), error) {
	select {
	case m.indexMu <- struct{}{}:
		return func() { <-m.indexMu }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *mcpCoordinator) WithSeries(ctx context.Context, ref refs.Series, fn func() error) error {
	unlock, err := m.acquireSeries(ctx, ref)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}

func (m *mcpCoordinator) WithIndex(ctx context.Context, fn func() error) error {
	unlock, err := m.acquireIndex(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}
