// Package indexfile owns reading and writing <library>/.kura/index.jsonl. The
// JSONL file is a materialized view: one header line plus one Row per library
// entry (tracked series and untracked directories). The Index value caches the
// view in memory; mutators rewrite the file via SaveCAS and reload via Load.
//
// Selector lookup (metadata-ref → series-ref) goes through Index.Get and is
// O(1). Phase 1 keeps the underlying map as metadata→series; phase 2 will
// split into bySeries/byMeta maps to support full-row reads. For now Rows
// returns sparse rows containing only Series + Metadata.
package indexfile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
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

	mu   sync.RWMutex
	refs map[refs.Metadata]refs.Series

	// Watch baseline. Tracks the on-disk file state so the probe loop
	// can detect peer mutations without re-reading the file every tick.
	cachedHash  string
	cachedMTime time.Time
	cachedSize  int64

	// log + reader are set by Watch before any loop starts, then
	// read-only thereafter. No locking needed for reads.
	log    Logger
	reader MetadataReader
}

func New(root string) *Index {
	return &Index{
		root: root,
		refs: map[refs.Metadata]refs.Series{},
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
	for _, row := range loaded.Rows {
		if row.Metadata == "" {
			continue // untracked rows have no metadata; skip for selector map
		}
		if err := index.Put(row.Metadata, row.Series); err != nil {
			return nil, fmt.Errorf("indexfile: load: %w", err)
		}
	}
	return index, nil
}

// Rebuild walks root for series subdirectories and asks read for each one's
// metadata ref. Series whose metadata is missing on disk are silently
// skipped. Progress events are reported under the "reindex" stage.
//
// Phase 1 still returns metadata→series; phase 2 widens this to a full
// RowBuilder.
func Rebuild(ctx context.Context, root string, read func(context.Context, refs.Series) (refs.Metadata, error)) (*Index, error) {
	progress.Start(ctx, "reindex", "Rebuilding library index", progress.TotalIndeterminate)
	dir, err := os.Open(root)
	if err != nil {
		progress.Failure(ctx, "reindex", "Failed to rebuild library index", 0, 0)
		return nil, err
	}
	defer dir.Close()

	index := New(root)
	scanned := 0
	for {
		entries, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == paths.KuraDir {
				continue
			}
			seriesRef, err := refs.ParseSeries(entry.Name())
			if err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
				return nil, err
			}
			scanned++
			progress.Update(ctx, "reindex", fmt.Sprintf("Indexing %s", seriesRef), scanned, progress.TotalIndeterminate)
			metadataRef, err := read(ctx, seriesRef)
			if err != nil {
				continue
			}
			if err := index.Put(metadataRef, seriesRef); err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, progress.TotalIndeterminate)
				return nil, err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	progress.Success(ctx, "reindex", fmt.Sprintf("Rebuilt library index (%d series)", len(index.refs)), scanned)
	return index, nil
}

// Len returns the number of metadata-ref → series-ref entries.
func (i *Index) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.refs)
}

func (i *Index) Get(ref refs.Metadata) (refs.Series, bool, error) {
	if ref == "" {
		return refs.Series{}, false, errors.New("indexfile: metadata ref is required")
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	seriesRef, ok := i.refs[ref]
	return seriesRef, ok, nil
}

func (i *Index) Put(metadataRef refs.Metadata, seriesRef refs.Series) error {
	if metadataRef == "" {
		return errors.New("indexfile: metadata ref is required")
	}
	if seriesRef.IsZero() {
		return errors.New("indexfile: series ref is required")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	existing, exists := i.refs[metadataRef]
	if exists && existing != seriesRef {
		return DuplicateRefError{Ref: metadataRef, Existing: existing, Next: seriesRef}
	}
	i.refs[metadataRef] = seriesRef
	return nil
}

func (i *Index) Remove(seriesRef refs.Series) {
	i.mu.Lock()
	defer i.mu.Unlock()
	for metadataRef, existing := range i.refs {
		if existing == seriesRef {
			delete(i.refs, metadataRef)
		}
	}
}

// Rows returns the in-memory entries as a sorted slice of sparse rows.
// Phase 1 only fills Series and Metadata; phase 2 carries full row data.
// Sort is by Series ref so output is byte-stable across rebuilds.
func (i *Index) Rows() []Row {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]Row, 0, len(i.refs))
	for metadataRef, seriesRef := range i.refs {
		out = append(out, Row{Series: seriesRef, Metadata: metadataRef})
	}
	sort.Slice(out, func(a, b int) bool {
		return out[a].Series.String() < out[b].Series.String()
	})
	return out
}

// ReplaceRows swaps the in-memory state with the metadata→series projection
// of rows. Untracked rows (Metadata == "") are dropped from the selector map.
// Used after a successful CAS write to keep the cached read view in sync.
func (i *Index) ReplaceRows(rows []Row) {
	next := make(map[refs.Metadata]refs.Series, len(rows))
	for _, row := range rows {
		if row.Metadata == "" {
			continue
		}
		next[row.Metadata] = row.Series
	}
	i.mu.Lock()
	i.refs = next
	i.mu.Unlock()
}

// Save writes the in-memory state to index.jsonl via SaveCAS in create-only
// mode. Used by tests and by bootstrap on a fresh library; production
// mutators go through SaveCAS directly.
func (i *Index) Save(mutator coord.Mutator) error {
	rows := i.Rows()
	return SaveCAS(i.root, "", rows, mutator)
}
