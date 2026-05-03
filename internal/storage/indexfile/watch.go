package indexfile

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
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

// MetadataReader returns the metadata ref recorded under the named
// series directory. Used by the rebuild loop. Same shape as the
// function passed to Rebuild.
type MetadataReader func(context.Context, refs.Series) (refs.Metadata, error)

// WatchConfig controls how aggressively Watch refreshes the in-memory
// index against <library>/.kura/index.tsv. Each interval set to 0
// disables the corresponding loop. At least one must be enabled or
// the cache goes stale forever after the first peer mutation.
type WatchConfig struct {
	// ProbeInterval gates the open+fstat+close fast probe. Catches
	// mtime+size changes from any peer mutation; the open() round-
	// trip bypasses NFS attribute-cache lag.
	ProbeInterval time.Duration
	// RefreshInterval gates the unconditional read+hash refresh.
	// Catches mtime+size collisions and any cosmetic mtime bump
	// (touch) that left the contents unchanged.
	RefreshInterval time.Duration
	// RebuildInterval gates the periodic full library rebuild.
	// Catches drift between index.tsv and per-series state.
	// Reader must be non-nil if this is > 0.
	RebuildInterval time.Duration

	// Reader is the metadata-ref accessor used by the rebuild loop.
	// Required if RebuildInterval > 0; ignored otherwise.
	Reader MetadataReader
	// Logger receives lifecycle warnings. nil means silent.
	Logger Logger
}

// Watch attaches the configured background freshness loops to this
// Index and returns immediately. All loops exit when ctx is cancelled.
// Calling Watch more than once is unsupported; the second call's
// reader/logger silently overwrite the first and an extra set of
// goroutines is spawned.
//
// Workflows continue to call ReplaceEntries directly after a
// successful CAS write; the next probe tick will detect the post-
// write mtime change and re-read the file (a no-op overwrite of the
// same map). Future commits may add a hook to skip the redundant read.
func (i *Index) Watch(ctx context.Context, cfg WatchConfig) {
	if cfg.Logger == nil {
		cfg.Logger = nopLogger{}
	}
	i.log = cfg.Logger
	i.reader = cfg.Reader

	// Seed baseline so the first probe tick doesn't falsely fire.
	if data, mtime, size, err := readIndexBytes(i.root); err == nil {
		i.mu.Lock()
		i.cachedHash = hashHex(data)
		i.cachedMTime = mtime
		i.cachedSize = size
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
			i.probeOnce()
		}
	}
}

func (i *Index) probeOnce() {
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
	if err := i.fullRefresh(); err != nil {
		i.log.Warn("indexfile: probe refresh", "err", err)
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
			if err := i.fullRefresh(); err != nil {
				i.log.Warn("indexfile: refresh", "err", err)
			}
		}
	}
}

// fullRefresh reads the index file unconditionally, hashes it, and
// replaces the in-memory entries iff the hash differs. The mtime+size
// baseline is updated regardless so cosmetic touches don't keep
// firing the probe.
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
	i.mu.RUnlock()
	if same {
		i.mu.Lock()
		i.cachedMTime = mtime
		i.cachedSize = size
		i.mu.Unlock()
		return nil
	}
	parsed, err := ParseCAS(data)
	if err != nil {
		return err
	}
	i.ReplaceEntries(parsed.Entries)
	i.mu.Lock()
	i.cachedHash = newHash
	i.cachedMTime = mtime
	i.cachedSize = size
	i.mu.Unlock()
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
			if err := i.rebuildOnce(ctx); err != nil {
				i.log.Warn("indexfile: rebuild", "err", err)
			}
		}
	}
}

// rebuildOnce walks the library, rebuilds the index from per-series
// metadata, and CAS-writes the result. A pre_write conflict means a
// peer wrote during the walk; skip and let the probe pick up the
// peer's version on the next tick.
func (i *Index) rebuildOnce(ctx context.Context) error {
	if i.reader == nil {
		return errors.New("indexfile: rebuild reader not set")
	}
	rebuilt, err := Rebuild(ctx, i.root, i.reader)
	if err != nil {
		return err
	}
	entries := rebuilt.Entries()

	i.mu.RLock()
	expected := i.cachedHash
	i.mu.RUnlock()

	if err := SaveCAS(i.root, expected, entries, coord.NewMutator("indexfile-rebuild")); err != nil {
		if _, ok := errors.AsType[*coord.ConflictError](err); ok {
			return nil
		}
		return err
	}
	i.ReplaceEntries(entries)
	if data, mtime, size, err := readIndexBytes(i.root); err == nil {
		i.mu.Lock()
		i.cachedHash = hashHex(data)
		i.cachedMTime = mtime
		i.cachedSize = size
		i.mu.Unlock()
	}
	return nil
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
