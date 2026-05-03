package indexfile

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// Logger is the small interface Watcher uses for lifecycle and error
// logs. Plugs into stdlib log or a structured logger.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
}

type nopLogger struct{}

func (nopLogger) Info(string, ...any) {}
func (nopLogger) Warn(string, ...any) {}

// MetadataReader returns the metadata ref recorded under the named
// series directory. Used by Watcher.rebuildOnce. Same shape as the
// function passed to Rebuild.
type MetadataReader func(context.Context, refs.Series) (refs.Metadata, error)

// WatcherConfig controls how aggressively the Watcher refreshes its
// view of <library>/.kura/index.tsv. Each interval set to 0 disables
// the corresponding loop. At least one must be enabled or the cache
// goes stale forever after the first peer mutation.
type WatcherConfig struct {
	// ProbeInterval gates the open+fstat+close fast probe. Catches
	// mtime+size changes from any peer mutation.
	ProbeInterval time.Duration
	// RefreshInterval gates the unconditional read+hash refresh.
	// Catches mtime/size collisions and NFS attribute-cache lag.
	RefreshInterval time.Duration
	// RebuildInterval gates the periodic full library rebuild.
	// Catches drift between index.tsv and per-series state.
	RebuildInterval time.Duration
}

// Watcher owns the long-lived cache of <library>/.kura/index.tsv. It
// wraps an existing *Index and refreshes its entries on three loops:
// a fast mtime+size probe, a periodic full-file refresh, and a
// periodic library-wide rebuild. Used by `kura serve`; CLI does not
// instantiate one.
//
// Workflows continue to call Index.ReplaceEntries directly after a
// successful CAS write; the next probe tick will detect the post-
// write mtime change and re-read the file (a no-op overwrite of the
// same map). The redundant work is acceptable; future commits may
// add a LocalMutationApplied hook that bypasses the next probe.
type Watcher struct {
	idx    *Index
	root   string
	reader MetadataReader
	cfg    WatcherConfig
	log    Logger

	mu    sync.Mutex
	hash  string
	mtime time.Time
	size  int64
}

// NewWatcher seeds a Watcher around an already-loaded Index. The
// initial hash/mtime/size baseline is read from disk so the first
// probe tick does not falsely fire. If the index file does not exist
// yet, the baseline is empty and the first write is treated as a
// change.
func NewWatcher(idx *Index, reader MetadataReader, cfg WatcherConfig, log Logger) *Watcher {
	if log == nil {
		log = nopLogger{}
	}
	w := &Watcher{
		idx:    idx,
		root:   idx.root,
		reader: reader,
		cfg:    cfg,
		log:    log,
	}
	if data, mtime, size, err := readIndexBytes(idx.root); err == nil {
		w.hash = hashHex(data)
		w.mtime = mtime
		w.size = size
	}
	return w
}

// Index returns the *Index this Watcher manages.
func (w *Watcher) Index() *Index { return w.idx }

// Run starts the enabled background loops and returns immediately.
// All loops exit when ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	if w.cfg.ProbeInterval > 0 {
		go w.probeLoop(ctx)
	}
	if w.cfg.RefreshInterval > 0 {
		go w.refreshLoop(ctx)
	}
	if w.cfg.RebuildInterval > 0 {
		go w.rebuildLoop(ctx)
	}
}

func (w *Watcher) probeLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.ProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.probeOnce()
		}
	}
}

func (w *Watcher) probeOnce() {
	path := paths.IndexFile(w.root)
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			w.log.Warn("indexfile: probe open", "err", err)
		}
		return
	}
	stat, statErr := f.Stat()
	f.Close()
	if statErr != nil {
		w.log.Warn("indexfile: probe stat", "err", statErr)
		return
	}
	w.mu.Lock()
	cachedMTime, cachedSize := w.mtime, w.size
	w.mu.Unlock()
	if stat.ModTime().Equal(cachedMTime) && stat.Size() == cachedSize {
		return
	}
	if err := w.fullRefresh(); err != nil {
		w.log.Warn("indexfile: probe refresh", "err", err)
	}
}

func (w *Watcher) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.fullRefresh(); err != nil {
				w.log.Warn("indexfile: refresh", "err", err)
			}
		}
	}
}

// fullRefresh reads the index file unconditionally, hashes it, and
// replaces the in-memory entries iff the hash differs. The mtime+size
// baseline is updated regardless so cosmetic touches don't keep
// firing the probe.
func (w *Watcher) fullRefresh() error {
	data, mtime, size, err := readIndexBytes(w.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	newHash := hashHex(data)
	w.mu.Lock()
	same := newHash == w.hash
	w.mu.Unlock()
	if same {
		w.mu.Lock()
		w.mtime = mtime
		w.size = size
		w.mu.Unlock()
		return nil
	}
	parsed, err := ParseCAS(data)
	if err != nil {
		return err
	}
	w.idx.ReplaceEntries(parsed.Entries)
	w.mu.Lock()
	w.hash = newHash
	w.mtime = mtime
	w.size = size
	w.mu.Unlock()
	return nil
}

func (w *Watcher) rebuildLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.RebuildInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.rebuildOnce(ctx); err != nil {
				w.log.Warn("indexfile: rebuild", "err", err)
			}
		}
	}
}

// rebuildOnce walks the library, rebuilds the index from per-series
// metadata, and CAS-writes the result. A pre_write conflict means a
// peer wrote during our walk; we skip and let the probe pick up the
// peer's version on the next tick.
func (w *Watcher) rebuildOnce(ctx context.Context) error {
	if w.reader == nil {
		return errors.New("indexfile: rebuild reader not set")
	}
	rebuilt, err := Rebuild(ctx, w.root, w.reader)
	if err != nil {
		return err
	}
	entries := rebuilt.Entries()

	w.mu.Lock()
	expected := w.hash
	w.mu.Unlock()

	if err := SaveCAS(w.root, expected, entries, coord.NewMutator("indexfile-rebuild")); err != nil {
		if _, ok := errors.AsType[*coord.ConflictError](err); ok {
			return nil
		}
		return err
	}
	w.idx.ReplaceEntries(entries)
	if data, mtime, size, err := readIndexBytes(w.root); err == nil {
		w.mu.Lock()
		w.hash = hashHex(data)
		w.mtime = mtime
		w.size = size
		w.mu.Unlock()
	}
	return nil
}

// readIndexBytes reads the index file plus the stat fields the
// Watcher tracks for its baseline.
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
