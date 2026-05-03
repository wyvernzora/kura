package coord

import (
	"sync"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

// Coordinator serializes mutations against the same series (or the
// library index) within a single process and bundles the standard
// CAS retry policy.
//
// Two impls exist:
//
//   - NewCLICoordinator returns a no-op serializer. The CLI binary
//     runs one mutation per invocation; intra-process serialization
//     buys nothing.
//
//   - NewMCPCoordinator holds a per-key sync.Mutex (sync.Map-backed)
//     so concurrent goroutine-served requests don't race the same
//     series. The serialization bracket includes the conflict retry
//     loop (the *Retry methods) so two attempts from the same goroutine
//     stay paired.
//
// Workflows accept a Coordinator from Deps and don't care which
// variant they have.
type Coordinator interface {
	// WithSeries serializes goroutines in-process; no retry. Use for
	// long ops that don't tolerate retry (reconcile apply) and ops
	// without a CAS write (trash empty/restore).
	WithSeries(ref refs.Series, fn func() error) error

	// WithIndex serializes index-touching ops in-process; no retry.
	WithIndex(fn func() error) error

	// WithSeriesRetry adds conflict retry on top of WithSeries.
	// For short ops + scan, where re-running fn is safe.
	WithSeriesRetry(ref refs.Series, fn func() error) error

	// WithIndexRetry adds conflict retry on top of WithIndex.
	WithIndexRetry(fn func() error) error
}

type config struct {
	maxAttempts int
}

// Option mutates a coord constructor's config.
type Option func(*config)

// MaxAttempts overrides the default retry count for *Retry methods.
// The value is the total attempt count (initial + retries), not the
// retry count alone. Defaults to DefaultMaxAttempts (= 2).
func MaxAttempts(n int) Option {
	return func(c *config) {
		if n < 1 {
			n = 1
		}
		c.maxAttempts = n
	}
}

func resolveConfig(opts []Option) config {
	cfg := config{maxAttempts: DefaultMaxAttempts}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// NewCLICoordinator returns a Coordinator with no in-process
// serialization. fn is invoked immediately under WithSeries/WithIndex;
// *Retry methods still apply RetryOnConflict.
func NewCLICoordinator(opts ...Option) Coordinator {
	return &cliCoordinator{cfg: resolveConfig(opts)}
}

// NewMCPCoordinator returns a Coordinator that serializes goroutines
// per-series (and globally for the index). Required for any long-
// running consumer that handles multiple concurrent requests.
func NewMCPCoordinator(opts ...Option) Coordinator {
	return &mcpCoordinator{cfg: resolveConfig(opts)}
}

type cliCoordinator struct {
	cfg config
}

func (c *cliCoordinator) WithSeries(_ refs.Series, fn func() error) error {
	return fn()
}

func (c *cliCoordinator) WithIndex(fn func() error) error {
	return fn()
}

func (c *cliCoordinator) WithSeriesRetry(_ refs.Series, fn func() error) error {
	return RetryOnConflict(c.cfg.maxAttempts, fn)
}

func (c *cliCoordinator) WithIndexRetry(fn func() error) error {
	return RetryOnConflict(c.cfg.maxAttempts, fn)
}

type mcpCoordinator struct {
	cfg     config
	series  sync.Map // map[refs.Series]*sync.Mutex
	indexMu sync.Mutex
}

func (m *mcpCoordinator) lockSeries(ref refs.Series) func() {
	value, _ := m.series.LoadOrStore(ref, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *mcpCoordinator) WithSeries(ref refs.Series, fn func() error) error {
	unlock := m.lockSeries(ref)
	defer unlock()
	return fn()
}

func (m *mcpCoordinator) WithIndex(fn func() error) error {
	m.indexMu.Lock()
	defer m.indexMu.Unlock()
	return fn()
}

func (m *mcpCoordinator) WithSeriesRetry(ref refs.Series, fn func() error) error {
	unlock := m.lockSeries(ref)
	defer unlock()
	return RetryOnConflict(m.cfg.maxAttempts, fn)
}

func (m *mcpCoordinator) WithIndexRetry(fn func() error) error {
	m.indexMu.Lock()
	defer m.indexMu.Unlock()
	return RetryOnConflict(m.cfg.maxAttempts, fn)
}
