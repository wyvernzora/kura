// Package indexfile owns reading and writing <library>/.kura/index.jsonl. The
// JSONL file is a materialized view: one header line plus one Row per library
// entry (tracked series and untracked directories). The Index value caches
// the view in memory; mutators rewrite the file via SaveCAS and reload via
// Load.
//
// The in-memory state is split between three structures kept in lockstep
// under one RWMutex:
//
//   - bySeries: primary, full-row map keyed by series ref.
//   - byMeta:   selector lookup (metadata-ref → series-ref), derived.
//   - order:    sorted seriesRef slice for List iteration; recomputed on
//     mutation. Sort key is (lower-cased title, series ref).
//
// Selector lookup (Index.Get) stays O(1). Full-row reads use GetRow / Rows.
package indexfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// ErrNotReady is returned by Snapshot when a rebuild is in flight and
// the in-memory map is empty (cold start / corruption recovery). Read
// callers translate this to a surface-specific not-ready error.
var ErrNotReady = errors.New("indexfile: not ready")

var ErrNotFound = errors.New("indexfile: not found")

type DuplicateRefError struct {
	Ref      refs.Metadata
	Existing refs.Series
	Next     refs.Series
}

func (e DuplicateRefError) Error() string {
	return fmt.Sprintf("indexfile: %s is already tracked at %q", e.Ref, e.Existing)
}

// Index is the in-memory view of index.jsonl. It is constructed empty by
// New, populated by Load or Rebuild, and persisted by Save / SaveCAS.
//
// Methods are safe for concurrent use: kura serve has multiple goroutines
// (request handlers + the Watch loops) reading and writing the same Index.
type Index struct {
	root string

	mu       sync.RWMutex
	bySeries map[refs.Series]Row
	byMeta   map[refs.Metadata]refs.Series
	order    []refs.Series

	// Watch baseline. Tracks the on-disk file state so the probe loop
	// can detect peer mutations without re-reading the file every tick.
	cachedHash         string
	cachedMTime        time.Time
	cachedSize         int64
	cachedLibRootMTime time.Time

	// rebuilding flips true while a TriggerRebuild goroutine is in
	// flight. rebuildWG lets callers (CLI bootstrap) block on the
	// in-flight rebuild via WaitReady.
	rebuilding atomic.Bool
	rebuildWG  sync.WaitGroup

	// log + reader are set by Watch before any loop starts, then
	// read-only thereafter. No locking needed for reads.
	log     Logger
	builder RowBuilder
}

func New(root string) *Index {
	return &Index{
		root:     root,
		bySeries: map[refs.Series]Row{},
		byMeta:   map[refs.Metadata]refs.Series{},
	}
}

// Load reads index.jsonl and returns a populated Index. Returns ErrNotFound
// when the file does not exist.
func Load(root string) (*Index, error) {
	loaded, err := LoadCAS(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	index := New(root)
	if err := index.Replace(loaded.Rows); err != nil {
		return nil, fmt.Errorf("indexfile: load: %w", err)
	}
	return index, nil
}

// Rebuild walks root, calls build for each parseable subdirectory, and
// returns a populated Index. Untracked directories (no series.json) are
// included as Status=untracked rows. Directories whose names don't parse
// as series refs are skipped silently — a single weirdly-named dir must
// not block the whole rebuild. Progress events fire under "reindex".
func Rebuild(ctx context.Context, root string, build RowBuilder) (*Index, error) {
	progress.Start(ctx, "reindex", "Rebuilding library index", progress.TotalIndeterminate)
	dir, err := os.Open(root)
	if err != nil {
		progress.Failure(ctx, "reindex", "Failed to rebuild library index", 0, 0)
		return nil, err
	}
	defer dir.Close()

	now := time.Now().UTC()
	index := New(root)
	scanned := 0
	for {
		entries, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") || name == paths.KuraDir {
				continue
			}
			seriesRef, parseErr := refs.ParseSeries(name)
			if parseErr != nil {
				continue
			}
			scanned++
			progress.Update(ctx, "reindex", fmt.Sprintf("Indexing %s", seriesRef), scanned, progress.TotalIndeterminate)
			row, err := build(root, seriesRef, now)
			if err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
				return nil, err
			}
			if err := index.Upsert(row); err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
				return nil, err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	progress.Success(ctx, "reindex", fmt.Sprintf("Rebuilt library index (%d series)", len(index.bySeries)), scanned)
	return index, nil
}

// Len returns the number of rows in memory (tracked + untracked).
func (i *Index) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.bySeries)
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

// GetRow returns the full row for a series ref. O(1).
func (i *Index) GetRow(ref refs.Series) (Row, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	row, ok := i.bySeries[ref]
	return row, ok
}

// Put inserts a sparse row containing only Series + Metadata. Rejects a
// metadata-ref already mapped to a different series with DuplicateRefError.
// Kept for selector-only callers (tests, simple bootstrap); production
// mutators use Upsert with full rows from BuildRow / BuildRowFromModel.
func (i *Index) Put(metadataRef refs.Metadata, seriesRef refs.Series) error {
	if metadataRef == "" {
		return errors.New("indexfile: metadata ref is required")
	}
	if seriesRef.IsZero() {
		return errors.New("indexfile: series ref is required")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if existing, ok := i.byMeta[metadataRef]; ok && existing != seriesRef {
		return DuplicateRefError{Ref: metadataRef, Existing: existing, Next: seriesRef}
	}
	row, ok := i.bySeries[seriesRef]
	if !ok {
		row = Row{Series: seriesRef, Title: seriesRef.String()}
	}
	if row.Metadata != "" && row.Metadata != metadataRef {
		delete(i.byMeta, row.Metadata)
	}
	row.Metadata = metadataRef
	i.bySeries[seriesRef] = row
	i.byMeta[metadataRef] = seriesRef
	i.recomputeOrderLocked()
	return nil
}

// Upsert inserts or replaces the row for row.Series. Rejects a
// metadata-ref already mapped to a different series.
func (i *Index) Upsert(row Row) error {
	if row.Series.IsZero() {
		return errors.New("indexfile: row series ref is required")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.upsertLocked(row)
}

func (i *Index) upsertLocked(row Row) error {
	prev, existed := i.bySeries[row.Series]
	if row.Metadata != "" {
		if winner, ok := i.byMeta[row.Metadata]; ok && winner != row.Series {
			return DuplicateRefError{Ref: row.Metadata, Existing: winner, Next: row.Series}
		}
	}
	if existed && prev.Metadata != "" && prev.Metadata != row.Metadata {
		delete(i.byMeta, prev.Metadata)
	}
	i.bySeries[row.Series] = row
	if row.Metadata != "" {
		i.byMeta[row.Metadata] = row.Series
	}
	i.recomputeOrderLocked()
	return nil
}

// Remove drops the row for seriesRef and any metadata mapping it carries.
// No-op if absent.
func (i *Index) Remove(seriesRef refs.Series) {
	i.mu.Lock()
	defer i.mu.Unlock()
	row, ok := i.bySeries[seriesRef]
	if !ok {
		return
	}
	delete(i.bySeries, seriesRef)
	if row.Metadata != "" {
		delete(i.byMeta, row.Metadata)
	}
	i.recomputeOrderLocked()
}

// Rows returns a snapshot of every row in memory, sorted by (title,
// seriesRef). Used by List and by callers that need the full materialized
// view.
func (i *Index) Rows() []Row {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]Row, 0, len(i.order))
	for _, ref := range i.order {
		out = append(out, i.bySeries[ref])
	}
	return out
}

// OrderedSeries returns the in-memory ordering as a slice of series refs.
// Used by pagination to compute the view hash without copying full rows.
func (i *Index) OrderedSeries() []refs.Series {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]refs.Series, len(i.order))
	copy(out, i.order)
	return out
}

// Replace swaps the in-memory state with the given rows in one shot.
// Used after a successful CAS load/write to keep the cached view in sync.
// Rejects internal duplicate metadata refs in the input.
func (i *Index) Replace(rows []Row) error {
	bySeries, byMeta, err := buildRowMaps(rows)
	if err != nil {
		return err
	}
	i.mu.Lock()
	i.applyMapsLocked(bySeries, byMeta)
	i.mu.Unlock()
	return nil
}

// ReplaceRows is the no-error variant of Replace used by hot paths that
// know their input is already deduped (CAS-write follow-ups). Falls back
// to Replace's logic but discards the duplicate-ref error; callers
// preferring strict checks should call Replace directly.
func (i *Index) ReplaceRows(rows []Row) {
	_ = i.Replace(rows)
}

// SaveAndAdopt is the sync-mutation entry point: writes rows via SaveCAS
// then, under one lock, swaps the in-memory rows AND bumps the probe
// cache (hash/mtime/size) so the next probe tick treats this write as
// already observed. Without this, every successful sync index mutation
// triggers a no-op fullRefresh on the next probe tick.
//
// expected mirrors SaveCAS: SHA-256 hex of the on-disk bytes the caller
// loaded before mutating; "" means "expect file does not exist".
//
// Cache stays unchanged on any error.
func (i *Index) SaveAndAdopt(expected string, rows []Row, mutator coord.Mutator) error {
	if err := SaveCAS(i.root, expected, rows, mutator); err != nil {
		return err
	}
	bySeries, byMeta, err := buildRowMaps(rows)
	if err != nil {
		return err
	}
	data, mtime, size, err := readIndexBytes(i.root)
	if err != nil {
		// Write succeeded but stat failed — extremely rare. Leave the
		// cache stale; the next probe tick will fullRefresh and recover.
		return err
	}
	i.mu.Lock()
	i.applyMapsLocked(bySeries, byMeta)
	i.cachedHash = hashHex(data)
	i.cachedMTime = mtime
	i.cachedSize = size
	i.mu.Unlock()
	return nil
}

func buildRowMaps(rows []Row) (map[refs.Series]Row, map[refs.Metadata]refs.Series, error) {
	bySeries := make(map[refs.Series]Row, len(rows))
	byMeta := make(map[refs.Metadata]refs.Series, len(rows))
	for _, row := range rows {
		if row.Series.IsZero() {
			continue
		}
		if row.Metadata != "" {
			if existing, ok := byMeta[row.Metadata]; ok && existing != row.Series {
				return nil, nil, DuplicateRefError{Ref: row.Metadata, Existing: existing, Next: row.Series}
			}
			byMeta[row.Metadata] = row.Series
		}
		bySeries[row.Series] = row
	}
	return bySeries, byMeta, nil
}

func (i *Index) applyMapsLocked(bySeries map[refs.Series]Row, byMeta map[refs.Metadata]refs.Series) {
	i.bySeries = bySeries
	i.byMeta = byMeta
	i.recomputeOrderLocked()
}

// Save writes the in-memory state to index.jsonl via SaveCAS in
// create-only mode. Used by tests and by bootstrap on a fresh library;
// production mutators go through SaveCAS directly.
func (i *Index) Save(mutator coord.Mutator) error {
	rows := i.Rows()
	return SaveCAS(i.root, "", rows, mutator)
}

// Snapshot returns a sorted slice of every row in memory. While a
// rebuild is in flight AND the map is empty (cold start, corruption
// recovery), returns ErrNotReady so read callers can surface a
// not-ready response. Once any prior snapshot exists, reads keep
// flowing through it during the rebuild.
func (i *Index) Snapshot() ([]Row, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	if i.rebuilding.Load() && len(i.bySeries) == 0 {
		return nil, ErrNotReady
	}
	out := make([]Row, 0, len(i.order))
	for _, ref := range i.order {
		out = append(out, i.bySeries[ref])
	}
	return out, nil
}

// Rebuilding reports whether a background rebuild is currently in flight.
func (i *Index) Rebuilding() bool {
	return i.rebuilding.Load()
}

// WaitReady blocks until any in-flight rebuild completes or ctx is
// cancelled. Used by CLI bootstrap to keep its sync semantics on top
// of the unified async rebuild mechanism.
func (i *Index) WaitReady(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		i.rebuildWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TriggerRebuild kicks off a background rebuild + SaveCAS + Replace.
// Idempotent: if a rebuild is already in flight, returns immediately
// without spawning another. The builder + mutator are captured by the
// in-flight goroutine; subsequent calls during a rebuild use the
// already-captured values.
//
// On success the in-memory rows are replaced and the watcher baseline
// is refreshed. On failure (rebuild error, parse error, peer-write
// conflict) the goroutine logs and exits without panicking; the next
// trigger will retry.
func (i *Index) TriggerRebuild(ctx context.Context, root string, builder RowBuilder, mutator coord.Mutator) {
	if !i.rebuilding.CompareAndSwap(false, true) {
		return
	}
	i.rebuildWG.Go(func() {
		defer i.rebuilding.Store(false)

		started := time.Now()
		if i.log != nil {
			i.log.Info("indexfile: rebuild starting", "op", mutator.Op)
		}

		rebuilt, err := Rebuild(ctx, root, builder)
		if err != nil {
			if i.log != nil {
				i.log.Warn("indexfile: rebuild", "err", err)
			}
			return
		}
		rows := rebuilt.Rows()

		i.mu.RLock()
		expected := i.cachedHash
		priorEntries := len(i.bySeries)
		i.mu.RUnlock()

		if err := i.SaveAndAdopt(expected, rows, mutator); err != nil {
			if _, ok := errors.AsType[*coord.ConflictError](err); !ok {
				if i.log != nil {
					i.log.Warn("indexfile: rebuild save", "err", err)
				}
				return
			}
			// Peer wrote during the walk; their state is on disk.
			// Adopt it via the probe path on the next tick. Don't
			// Replace our stale view here.
			if i.log != nil {
				i.log.Info("indexfile: rebuild conflicted with peer; deferring to next probe",
					"duration_ms", time.Since(started).Milliseconds(),
				)
			}
			return
		}

		if i.log != nil {
			i.log.Info("indexfile: rebuild complete",
				"priorEntries", priorEntries,
				"newEntries", len(rows),
				"duration_ms", time.Since(started).Milliseconds(),
			)
		}
	})
}

// recomputeOrderLocked rebuilds the sorted order slice. Sort key is
// (lower-cased title, series ref) for stable, case-insensitive
// alphabetical ordering. Caller must hold i.mu.
func (i *Index) recomputeOrderLocked() {
	order := make([]refs.Series, 0, len(i.bySeries))
	for ref := range i.bySeries {
		order = append(order, ref)
	}
	sort.Slice(order, func(a, b int) bool {
		ta := strings.ToLower(i.bySeries[order[a]].Title)
		tb := strings.ToLower(i.bySeries[order[b]].Title)
		if ta != tb {
			return ta < tb
		}
		return order[a].String() < order[b].String()
	})
	i.order = order
}
