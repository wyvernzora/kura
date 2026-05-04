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
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

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
	cachedHash  string
	cachedMTime time.Time
	cachedSize  int64

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
	bySeries := make(map[refs.Series]Row, len(rows))
	byMeta := make(map[refs.Metadata]refs.Series, len(rows))
	for _, row := range rows {
		if row.Series.IsZero() {
			continue
		}
		if row.Metadata != "" {
			if existing, ok := byMeta[row.Metadata]; ok && existing != row.Series {
				return DuplicateRefError{Ref: row.Metadata, Existing: existing, Next: row.Series}
			}
			byMeta[row.Metadata] = row.Series
		}
		bySeries[row.Series] = row
	}
	i.mu.Lock()
	i.bySeries = bySeries
	i.byMeta = byMeta
	i.recomputeOrderLocked()
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

// Save writes the in-memory state to index.jsonl via SaveCAS in
// create-only mode. Used by tests and by bootstrap on a fresh library;
// production mutators go through SaveCAS directly.
func (i *Index) Save(mutator coord.Mutator) error {
	rows := i.Rows()
	return SaveCAS(i.root, "", rows, mutator)
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
