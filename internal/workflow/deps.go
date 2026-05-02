// Package workflow holds the surface-agnostic operations that CLI and MCP
// surfaces compose. Every workflow is a stateless package-level function
// matching:
//
//	func Op(ctx context.Context, deps Deps, in OpInput) (response.OpResult, error)
//
// Deps is constructed once at startup (cmd/kura/build.go,
// cmd/kura-mcp/build.go) and passed by value to every workflow call. Do
// not stash state across calls; workflows execute, return, and forget.
package workflow

import (
	"sync"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// Deps is the cross-workflow dependency set. A field belongs here only if
// at least three workflows need it; otherwise pass narrow needs through
// the workflow's input struct. Reconsider splitting at ~7 fields.
type Deps struct {
	// LibRoot is the absolute filesystem path to the Kura library root.
	LibRoot string

	// Index is the in-memory metadata-ref → series-ref cache, loaded at
	// startup. Workflows mutate it via Put/Remove and persist via Save.
	Index *indexfile.Index

	// Provider yields a provider.Source on first call and caches the
	// result. Local-only workflows never invoke it; provider-needing
	// workflows call deps.Provider() and surface a missing-key error
	// only when the provider is actually required.
	Provider ProviderFactory

	// Inspector is the mediainfo wrapper for probing media files.
	Inspector media.Inspector

	// Now returns the current time. Tests inject a fixed clock.
	Now func() time.Time
}

// ProviderFactory constructs a provider.Source on demand. Wrap the actual
// constructor with NewProviderFactory so repeated calls share one
// provider.Source (and one missing-key error if construction fails).
type ProviderFactory func() (provider.Source, error)

// NewProviderFactory caches the result of construct so it runs at most
// once per process. construct typically reads KURA_TVDB_KEY and builds a
// TVDB client; deferring the call lets local-only commands run without
// the key.
func NewProviderFactory(construct func() (provider.Source, error)) ProviderFactory {
	var (
		once sync.Once
		src  provider.Source
		err  error
	)
	return func() (provider.Source, error) {
		once.Do(func() {
			src, err = construct()
		})
		return src, err
	}
}
