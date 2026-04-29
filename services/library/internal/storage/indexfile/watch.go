package indexfile

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// Logger is the small interface Watch uses for lifecycle and error
// logs. Plugs into stdlib log or a structured logger.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

type nopLogger struct{}

func (nopLogger) Info(string, ...any) {}
func (nopLogger) Warn(string, ...any) {}

// WatchConfig controls how aggressively Watch refreshes the in-memory
// index against <library>/.kura/index.jsonl. Each interval set to 0
// disables the corresponding loop. At least one must be enabled or
// the cache goes stale forever after the first peer mutation.
type WatchConfig struct {
	// ProbeInterval gates the combined libRoot + index.jsonl fast
	// probe. A libRoot mtime change (mkdir / rmdir under the library)
	// triggers an async rebuild; an index.jsonl mtime / size change
	// (peer mutation) triggers a fullRefresh. The probe skips entirely
	// while a rebuild is already in flight.
	ProbeInterval time.Duration
	// RefreshInterval gates the unconditional read+hash refresh.
	// Catches mtime+size collisions and any cosmetic mtime bump
	// (touch) that left the contents unchanged.
	RefreshInterval time.Duration
	// RebuildInterval gates the periodic full library rebuild.
	// Catches drift between index.jsonl and per-series state.
	// Builder must be non-nil if this is > 0.
	RebuildInterval time.Duration

	// Builder is the row builder used by the rebuild loop and the
	// libRoot-trigger rebuild. Required if RebuildInterval > 0 or
	// ProbeInterval > 0; ignored otherwise.
	Builder RowBuilder
	// Logger receives lifecycle warnings. nil means silent.
	Logger Logger
}

// Watch attaches the configured background freshness loops to this
// Index and returns immediately. All loops exit when ctx is cancelled.
// Calling Watch more than once is unsupported; the second call's
// reader/logger silently overwrite the first and an extra set of
// goroutines is spawned.
//
// Workflows continue to call ReplaceRows directly after a successful
// CAS write; the next probe tick will detect the post-write mtime
// change and re-read the file (a no-op overwrite of the same map).
func (i *Index) Watch(ctx context.Context, cfg WatchConfig) {
	if cfg.Logger == nil {
		cfg.Logger = nopLogger{}
	}
	i.log = cfg.Logger
	i.builder = cfg.Builder

	// Seed the index baseline so the first probe tick doesn't falsely fire.
	// On schema-mismatched bytes we still seed (so the probe doesn't re-fire
	// every tick on the same unparseable file) and immediately trigger a
	// rebuild — the steady-state mismatch path expects callers to overwrite
	// via TriggerRebuild rather than wait for a probe tick.
	if data, mtime, size, err := readIndexBytes(i.root); err == nil {
		i.mu.Lock()
		i.cachedHash = hashHex(data)
		i.cachedMTime = mtime
		i.cachedSize = size
		i.mu.Unlock()
		if _, parseErr := ParseCAS(data); errors.Is(parseErr, ErrSchemaMismatch) && cfg.Builder != nil {
			cfg.Logger.Warn("indexfile: schema mismatch on watch seed, triggering rebuild", "err", parseErr)
			i.TriggerRebuild(ctx, i.root, cfg.Builder, coord.NewMutator("rebuild_corruption"))
		}
	}
	// Seed the libRoot baseline. Errors are non-fatal: probe will
	// pick up the first observed mtime as a baseline on the next tick.
	if stat, err := os.Stat(i.root); err == nil {
		i.mu.Lock()
		i.cachedLibRootMTime = stat.ModTime()
		i.mu.Unlock()
	}

	if cfg.ProbeInterval > 0 {
		go i.probeLoop(ctx, cfg.ProbeInterval)
	}
	if cfg.RefreshInterval > 0 {
		go i.refreshLoop(ctx, cfg.RefreshInterval)
	}
	if cfg.RebuildInterval > 0 {
		go i.rebuildLoop(ctx, cfg.RebuildInterval)
	}
}

func (i *Index) probeLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.probeOnce(ctx)
		}
	}
}

// probeOnce checks the libRoot mtime first (cheap stat). A bump fires
// an async rebuild and skips the index probe — the rebuild will rewrite
// the JSONL anyway. Otherwise the existing open+fstat fast probe on
// index.jsonl detects peer mutations.
//
// Skips entirely while a rebuild is in flight: the probe would race
// with the goroutine writing the JSONL, and rebuild covers anything
// the probe would have caught.
func (i *Index) probeOnce(ctx context.Context) {
	if i.Rebuilding() {
		return
	}

	if libStat, err := os.Stat(i.root); err == nil {
		i.mu.RLock()
		cached := i.cachedLibRootMTime
		i.mu.RUnlock()
		if !libStat.ModTime().Equal(cached) {
			i.mu.Lock()
			i.cachedLibRootMTime = libStat.ModTime()
			i.mu.Unlock()
			if i.builder != nil {
				i.TriggerRebuild(ctx, i.root, i.builder, coord.NewMutator("rebuild_libroot"))
			}
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		i.log.Warn("indexfile: probe libroot stat", "err", err)
	}

	path := paths.IndexFile(i.root)
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			i.log.Warn("indexfile: probe open", "err", err)
		}
		return
	}
	stat, statErr := f.Stat()
	f.Close()
	if statErr != nil {
		i.log.Warn("indexfile: probe stat", "err", statErr)
		return
	}
	i.mu.RLock()
	cachedMTime, cachedSize := i.cachedMTime, i.cachedSize
	i.mu.RUnlock()
	if stat.ModTime().Equal(cachedMTime) && stat.Size() == cachedSize {
		return
	}
	i.log.Info("indexfile: probe detected change, reloading",
		"oldMTime", cachedMTime,
		"newMTime", stat.ModTime(),
		"oldSize", cachedSize,
		"newSize", stat.Size(),
	)
	if err := i.fullRefresh(); err != nil {
		i.handleRefreshError(ctx, "probe refresh", err)
	}
}

func (i *Index) refreshLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if i.Rebuilding() {
				continue
			}
			if err := i.fullRefresh(); err != nil {
				i.handleRefreshError(ctx, "refresh", err)
			}
		}
	}
}

// handleRefreshError translates fullRefresh errors into either a
// log line (transient) or a triggered rebuild (schema mismatch).
// Centralizes the logic shared by probeOnce and refreshLoop so
// schema-mismatch handling can't drift between them.
func (i *Index) handleRefreshError(ctx context.Context, stage string, err error) {
	if errors.Is(err, ErrSchemaMismatch) {
		i.log.Warn("indexfile: schema mismatch on disk, triggering rebuild",
			"stage", stage,
			"err", err,
		)
		if i.builder != nil {
			i.TriggerRebuild(ctx, i.root, i.builder, coord.NewMutator("rebuild_corruption"))
		}
		return
	}
	i.log.Warn("indexfile: "+stage, "err", err)
}

// fullRefresh reads the index file unconditionally, hashes it, and
// replaces the in-memory entries iff the hash differs. The mtime+size
// baseline is updated regardless so cosmetic touches don't keep
// firing the probe.
//
// On ErrSchemaMismatch the cache hash/mtime/size are still bumped to
// the stale file's values so subsequent probe ticks don't re-fire on
// the same unparseable bytes. Callers translate the error into a
// TriggerRebuild call; the rebuild's SaveCAS uses the bumped cached
// hash as `expected` and CAS-overwrites the stale file in place.
// In-memory rows are intentionally left intact — they predate the
// schema-future write and remain the best-effort view until rebuild
// completes.
func (i *Index) fullRefresh() error {
	data, mtime, size, err := readIndexBytes(i.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	newHash := hashHex(data)
	i.mu.RLock()
	same := newHash == i.cachedHash
	priorEntries := len(i.bySeries)
	i.mu.RUnlock()
	if same {
		i.mu.Lock()
		i.cachedMTime = mtime
		i.cachedSize = size
		i.mu.Unlock()
		return nil
	}
	parsed, parseErr := ParseCAS(data)
	if parseErr != nil {
		if errors.Is(parseErr, ErrSchemaMismatch) {
			i.mu.Lock()
			i.cachedHash = newHash
			i.cachedMTime = mtime
			i.cachedSize = size
			i.mu.Unlock()
		}
		return parseErr
	}
	i.ReplaceRows(parsed.Rows)
	i.mu.Lock()
	i.cachedHash = newHash
	i.cachedMTime = mtime
	i.cachedSize = size
	i.mu.Unlock()
	i.log.Info("indexfile: reloaded from disk",
		"priorEntries", priorEntries,
		"newEntries", len(parsed.Rows),
		"size", size,
	)
	return nil
}

func (i *Index) rebuildLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if i.builder == nil {
				continue
			}
			i.TriggerRebuild(ctx, i.root, i.builder, coord.NewMutator("rebuild_periodic"))
		}
	}
}

// readIndexBytes reads the index file plus the stat fields the
// Watch baseline tracks.
func readIndexBytes(root string) ([]byte, time.Time, int64, error) {
	path := paths.IndexFile(root)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, 0, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, 0, err
	}
	return data, stat.ModTime(), stat.Size(), nil
}
