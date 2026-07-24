// Package workflow holds the surface-agnostic operations that CLI and MCP
// surfaces compose. Every workflow is a stateless package-level function
// matching:
//
//	func Op(ctx context.Context, deps Deps, in OpInput) (api.OpResult, error)
//
// Deps is constructed once at startup (cmd/kura-library-manager/build.go) and passed by value
// to every workflow call. Do not stash state across calls; workflows execute,
// return, and forget.
package workflow

import (
	"log/slog"
	"sync"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/media"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/provider"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/indexfile"
)

// Deps is the cross-workflow dependency set. A field belongs here only if
// at least three workflows need it; otherwise pass narrow needs through
// the workflow's input struct. Reconsider splitting at ~7 fields.
type Deps struct {
	// LibRoot is the absolute filesystem path to the Kura library root.
	LibRoot string

	// InboxRoot is the absolute filesystem path to the inbox where new
	// media drops before being staged into the library. Used by
	// inbox.Selector resolution and the inbox listing workflow.
	InboxRoot string

	// Index is the in-memory metadata-ref → series-ref cache loaded at
	// startup. Workflows resolve refs through it; mutations update the
	// source snapshot after the owning series.json write succeeds.
	Index *indexfile.Index

	// Coordinator serializes mutations against the same series within
	// this process; its WithIndex mutex is also wired into the Index as
	// the guard around snapshot writes. kura serve constructs the
	// serializing variant; tests use the no-op one.
	Coordinator coord.Coordinator

	// Provider yields a provider.Source on first call and caches the
	// result. Local-only workflows never invoke it; provider-needing
	// workflows call deps.Provider() and surface a missing-key error
	// only when the provider is actually required.
	Provider ProviderFactory

	// Inspector is the mediainfo wrapper for probing media files.
	Inspector media.Inspector

	// Now returns the current time. Tests inject a fixed clock.
	Now func() time.Time

	// Jobs is the registry that backs long workflows (Scan,
	// ApplyReconcile). Constructed once per process; CLI uses a
	// per-invocation registry, kura serve uses a long-lived one
	// shared by all transports.
	Jobs *jobs.Registry

	// Logger is the structured logger workflows write to for audit
	// events (file moves, etc.). Optional — nil disables logging.
	// CLI runs leave it nil; kura serve sets it.
	Logger *slog.Logger

	// PreferredLanguages contains ordered BCP-47 base forms. Used by the
	// searchkey fold to gate which translated titles flow into a series's
	// `searchKey` blob. Empty disables the translation channel — only
	// ASCII aliases + user aliases contribute to searchKey.
	PreferredLanguages []string

	// RowBuildOptions controls list/show row projection policy. Nil uses
	// Kura defaults; cmd/kura-library-manager sets this from deploy-time config.
	RowBuildOptions *indexfile.BuildOptions

	// ConflictAttempts is the total number of attempts for short CAS
	// mutations: one initial attempt plus configured retries. Zero uses
	// coord.DefaultMaxAttempts.
	ConflictAttempts int
}

func rowBuildOptions(deps Deps) indexfile.BuildOptions {
	if deps.RowBuildOptions == nil {
		return indexfile.DefaultBuildOptions()
	}
	return *deps.RowBuildOptions
}

func conflictAttempts(deps Deps) int {
	if deps.ConflictAttempts < 1 {
		return coord.DefaultMaxAttempts
	}
	return deps.ConflictAttempts
}

// logFileMove emits one structured log line per filesystem move
// performed by a workflow. Op identifies the workflow ("reconcile",
// "trash_add", "trash_restore"); the remaining attrs identify the
// move (typically ref/from/to/role). No-op when deps.Logger is nil.
func logFileMove(deps Deps, op string, attrs ...any) {
	if deps.Logger == nil {
		return
	}
	deps.Logger.Info("file move", append([]any{"op", op}, attrs...)...)
}

// ProviderFactory constructs a provider.Source on demand. Wrap the actual
// constructor with NewProviderFactory so repeated successful calls share
// one provider.Source. Construction errors are not cached: a retry after
// a fix (env set, network restored) gets a fresh attempt.
type ProviderFactory func() (provider.Source, error)

// NewProviderFactory caches the result of construct on success only.
// First call invokes construct; on success, the source is cached and
// returned by every later call. On error, the cache stays empty so the
// next call retries. construct typically reads KURA_TVDB_KEY and builds
// a TVDB client; deferring the call lets local-only commands run without
// the key.
func NewProviderFactory(construct func() (provider.Source, error)) ProviderFactory {
	var (
		mu     sync.Mutex
		src    provider.Source
		cached bool
	)
	return func() (provider.Source, error) {
		mu.Lock()
		defer mu.Unlock()
		if cached {
			return src, nil
		}
		s, err := construct()
		if err != nil {
			return nil, err
		}
		src = s
		cached = true
		return src, nil
	}
}
