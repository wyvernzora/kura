// Package indexfile owns reading and writing <library>/.kura/index.jsonl. The
// JSONL file is a source-data snapshot: one header line plus one entry per
// library directory. Tracked entries carry compact series.json wire data;
// untracked and error entries carry only the series ref plus optional error.
//
// The Index value keeps decoded entries in memory and projects Rows on demand.
// Methods are safe for concurrent use.
package indexfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// ErrNotReady is returned by Snapshot when a rebuild is in flight and
// the in-memory map is empty (cold start / corruption recovery). Read
// callers translate this to a surface-specific not-ready error.
var ErrNotReady = errors.New("indexfile: not ready")

var ErrNotFound = errors.New("indexfile: not found")

// ErrSchemaMismatch is returned when the on-disk header carries a schema
// version this build cannot read.
var ErrSchemaMismatch = errors.New("indexfile: schema version mismatch")

type DuplicateRefError struct {
	Ref      refs.Metadata
	Existing refs.Series
	Next     refs.Series
}

func (e DuplicateRefError) Error() string {
	return fmt.Sprintf("indexfile: %s is already tracked at %q", e.Ref, e.Existing)
}

// GuardFunc serializes index writes. nil means run inline.
type GuardFunc func(context.Context, func() error) error

type Config struct {
	BuildOptions BuildOptions
	Now          func() time.Time
	Guard        GuardFunc
	// Logger receives rebuild lifecycle logs. nil means silent. Set at
	// construction and immutable afterwards so background rebuild
	// goroutines never race a later assignment.
	Logger Logger
}

// Entry is one source-data index entry. Model means tracked; Error means a
// broken tracked directory; neither means untracked.
type Entry struct {
	Series refs.Series
	Model  *series.Series
	Error  string
}

type entry struct {
	series refs.Series
	model  *series.Series
	raw    json.RawMessage
	err    string
}

type entryBuilder func(context.Context, string, refs.Series) (Entry, error)

// Index is the in-memory view of index.jsonl.
type Index struct {
	root string

	mu      sync.RWMutex
	entries map[refs.Series]entry
	byMeta  map[refs.Metadata]refs.Series
	order   []refs.Series

	rebuilding atomic.Bool

	libRootRebuildTimer *time.Timer
	libRootDebounce     time.Duration
	cachedLibRootMTime  time.Time

	rebuildMu    sync.Mutex
	rebuildDone  chan struct{}
	buildOptions BuildOptions
	now          func() time.Time
	guard        GuardFunc
	builder      entryBuilder
	log          Logger
}

func New(root string, cfg Config) *Index {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	guard := cfg.Guard
	if guard == nil {
		guard = func(_ context.Context, fn func() error) error { return fn() }
	}
	log := cfg.Logger
	if log == nil {
		log = nopLogger{}
	}
	return &Index{
		root:         root,
		entries:      map[refs.Series]entry{},
		byMeta:       map[refs.Metadata]refs.Series{},
		buildOptions: cfg.BuildOptions,
		now:          now,
		guard:        guard,
		builder:      diskEntryBuilder,
		log:          log,
	}
}

// Load reads index.jsonl and returns a populated Index. Returns ErrNotFound
// when the file does not exist.
func Load(root string, cfg Config) (*Index, error) {
	entries, err := readSnapshot(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	index := New(root, cfg)
	if err := index.replace(entries); err != nil {
		return nil, fmt.Errorf("indexfile: load: %w", err)
	}
	return index, nil
}

// Len returns the number of entries in memory (tracked + untracked + error).
func (i *Index) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.entries)
}

// Get returns the series ref tracking the given metadata ref. O(1).
func (i *Index) Get(ref refs.Metadata) (refs.Series, bool, error) {
	if ref == "" {
		return refs.Series{}, false, errors.New("indexfile: metadata ref is required")
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	seriesRef, ok := i.byMeta[ref]
	return seriesRef, ok, nil
}

// GetRow returns the projected row for a series ref. O(1).
func (i *Index) GetRow(ref refs.Series) (Row, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	e, ok := i.entries[ref]
	if !ok {
		return Row{}, false
	}
	return i.rowForEntry(e), true
}

// Upsert inserts or replaces an in-memory entry. It does not persist.
func (i *Index) Upsert(in Entry) error {
	e, err := i.prepareEntry(in)
	if err != nil {
		return err
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.upsertLocked(e)
}

// Remove drops the in-memory entry for seriesRef and any metadata mapping it
// carries. No-op if absent. It does not persist.
func (i *Index) Remove(seriesRef refs.Series) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.removeLocked(seriesRef)
}

// SaveModel updates the in-memory model entry and persists the snapshot.
func (i *Index) SaveModel(ctx context.Context, model *series.Series, mutator coord.Mutator) error {
	return i.guard(ctx, func() error {
		if err := i.Upsert(Entry{Model: model}); err != nil {
			return err
		}
		return i.persist(mutator)
	})
}

// Delete removes an entry from memory and persists the snapshot.
func (i *Index) Delete(ctx context.Context, ref refs.Series, mutator coord.Mutator) error {
	return i.guard(ctx, func() error {
		i.Remove(ref)
		return i.persist(mutator)
	})
}

// Rows returns a projected snapshot of every row, sorted by title/ref.
func (i *Index) Rows() []Row {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]Row, 0, len(i.order))
	for _, ref := range i.order {
		out = append(out, i.rowForEntry(i.entries[ref]))
	}
	return out
}

// Snapshot returns sorted projected rows, or ErrNotReady during a cold rebuild.
func (i *Index) Snapshot() ([]Row, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.rebuilding.Load() && len(i.entries) == 0 {
		return nil, ErrNotReady
	}
	out := make([]Row, 0, len(i.order))
	for _, ref := range i.order {
		out = append(out, i.rowForEntry(i.entries[ref]))
	}
	return out, nil
}

func (i *Index) Rebuilding() bool {
	return i.rebuilding.Load()
}

func (i *Index) WaitReady(ctx context.Context) error {
	i.rebuildMu.Lock()
	done := i.rebuildDone
	i.rebuildMu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RebuildNow walks the library, swaps the in-memory entries, and persists.
func (i *Index) RebuildNow(ctx context.Context, op string) error {
	return i.guard(ctx, func() error {
		entries, err := i.buildEntries(ctx)
		if err != nil {
			return err
		}
		if err := i.replace(entries); err != nil {
			return err
		}
		return i.persist(coord.NewMutator(op))
	})
}

// TriggerRebuild kicks off an idempotent background rebuild.
func (i *Index) TriggerRebuild(ctx context.Context, op string) {
	if !i.rebuilding.CompareAndSwap(false, true) {
		return
	}
	done := make(chan struct{})
	i.rebuildMu.Lock()
	i.rebuildDone = done
	i.rebuildMu.Unlock()
	go func() {
		defer func() {
			i.rebuildMu.Lock()
			if i.rebuildDone == done {
				close(done)
				i.rebuildDone = nil
			}
			i.rebuildMu.Unlock()
			i.rebuilding.Store(false)
		}()
		started := time.Now()
		if i.log != nil {
			i.log.Info("indexfile: rebuild starting", "op", op)
		}
		prior := i.Len()
		if err := i.RebuildNow(ctx, op); err != nil {
			if i.log != nil {
				i.log.Warn("indexfile: rebuild", "err", err)
			}
			return
		}
		if i.log != nil {
			i.log.Info("indexfile: rebuild complete",
				"priorEntries", prior,
				"newEntries", i.Len(),
				"duration_ms", time.Since(started).Milliseconds(),
			)
		}
	}()
}

func (i *Index) buildEntries(ctx context.Context) (map[refs.Series]entry, error) {
	progress.Start(ctx, "reindex", "Rebuilding library index", progress.TotalIndeterminate)
	slog.Info("indexfile: rebuild starting", "root", i.root)
	dir, err := os.Open(i.root)
	if err != nil {
		progress.Failure(ctx, "reindex", "Failed to rebuild library index", 0, 0)
		return nil, err
	}
	defer dir.Close()

	out := map[refs.Series]entry{}
	scanned := 0
	for {
		if err := ctx.Err(); err != nil {
			progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
			return nil, err
		}
		dirents, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
			return nil, err
		}
		for _, dirent := range dirents {
			if err := ctx.Err(); err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
				return nil, err
			}
			name := dirent.Name()
			if !dirent.IsDir() || strings.HasPrefix(name, ".") || name == paths.KuraDir {
				continue
			}
			ref, parseErr := refs.ParseSeries(name)
			if parseErr != nil {
				continue
			}
			scanned++
			progress.Update(ctx, "reindex", fmt.Sprintf("Indexing %s", ref), scanned, progress.TotalIndeterminate)
			in, err := i.builder(ctx, i.root, ref)
			if err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
				return nil, err
			}
			e, err := i.prepareEntry(in)
			if err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
				return nil, err
			}
			out[e.series] = e
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	progress.Success(ctx, "reindex", fmt.Sprintf("Rebuilt library index (%d series)", len(out)), scanned)
	slog.Info("indexfile: rebuild complete", "root", i.root, "rows", len(out), "scanned", scanned)
	return out, nil
}

func (i *Index) prepareEntry(in Entry) (entry, error) {
	ref := in.Series
	if ref.IsZero() && in.Model != nil {
		ref = in.Model.Ref
	}
	if ref.IsZero() {
		return entry{}, errors.New("indexfile: entry series ref is required")
	}
	if in.Model == nil {
		return entry{series: ref, err: in.Error}, nil
	}
	model := *in.Model
	model.Ref = ref
	raw, err := seriesfile.Encode(i.root, &model)
	if err != nil {
		return entry{}, err
	}
	copyModel, err := seriesfile.Decode(i.root, ref, raw)
	if err != nil {
		return entry{}, err
	}
	return entry{series: ref, model: copyModel, raw: append(json.RawMessage(nil), raw...)}, nil
}

func (i *Index) replace(entries map[refs.Series]entry) error {
	byMeta := map[refs.Metadata]refs.Series{}
	for ref, e := range entries {
		if e.model == nil || e.model.Metadata == "" {
			continue
		}
		if existing, ok := byMeta[e.model.Metadata]; ok && existing != ref {
			return DuplicateRefError{Ref: e.model.Metadata, Existing: existing, Next: ref}
		}
		byMeta[e.model.Metadata] = ref
	}
	i.mu.Lock()
	i.entries = entries
	i.byMeta = byMeta
	i.recomputeOrderLocked()
	i.mu.Unlock()
	return nil
}

func (i *Index) upsertLocked(e entry) error {
	prev, existed := i.entries[e.series]
	if e.model != nil && e.model.Metadata != "" {
		if winner, ok := i.byMeta[e.model.Metadata]; ok && winner != e.series {
			return DuplicateRefError{Ref: e.model.Metadata, Existing: winner, Next: e.series}
		}
	}
	if existed && prev.model != nil && prev.model.Metadata != "" {
		delete(i.byMeta, prev.model.Metadata)
	}
	i.entries[e.series] = e
	if e.model != nil && e.model.Metadata != "" {
		i.byMeta[e.model.Metadata] = e.series
	}
	i.recomputeOrderLocked()
	return nil
}

func (i *Index) removeLocked(ref refs.Series) {
	e, ok := i.entries[ref]
	if !ok {
		return
	}
	delete(i.entries, ref)
	if e.model != nil && e.model.Metadata != "" {
		delete(i.byMeta, e.model.Metadata)
	}
	i.recomputeOrderLocked()
}

func (i *Index) persist(mutator coord.Mutator) error {
	i.mu.RLock()
	entries := make([]entry, 0, len(i.order))
	for _, ref := range i.order {
		entries = append(entries, i.entries[ref])
	}
	i.mu.RUnlock()
	return writeSnapshot(i.root, entries, mutator)
}

func (i *Index) rowForEntry(e entry) Row {
	now := i.now().UTC()
	if e.model != nil {
		return BuildRowFromModelWithOptions(e.model, now, i.buildOptions)
	}
	if e.err != "" {
		return Row{
			Series: e.series,
			Title:  e.series.String(),
			Status: response.ListStatusError,
			Error:  e.err,
		}
	}
	return UntrackedRow(e.series, now)
}

func (i *Index) recomputeOrderLocked() {
	order := make([]refs.Series, 0, len(i.entries))
	for ref := range i.entries {
		order = append(order, ref)
	}
	sort.Slice(order, func(a, b int) bool {
		ta := strings.ToLower(entryTitle(i.entries[order[a]]))
		tb := strings.ToLower(entryTitle(i.entries[order[b]]))
		if ta != tb {
			return ta < tb
		}
		return order[a].String() < order[b].String()
	})
	i.order = order
}

func entryTitle(e entry) string {
	if e.model == nil {
		return e.series.String()
	}
	if !e.model.PreferredTitle.IsZero() {
		return e.model.PreferredTitle.String()
	}
	return e.series.String()
}
