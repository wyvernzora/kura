// Package indexfile owns reading and writing <library>/.kura/index.tsv. It
// holds the metadata-ref → series-ref map used by selectors. The Index value
// is the in-memory cache mutated by Get/Put/Remove and persisted by Save.
package indexfile

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/google/renameio/v2"
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

// Index is the in-memory map of metadata refs to series refs. It is
// constructed empty by New, populated by Load or Rebuild, and persisted by
// Save.
type Index struct {
	root string
	refs map[refs.Metadata]refs.Series
}

func New(root string) *Index {
	return &Index{
		root: root,
		refs: map[refs.Metadata]refs.Series{},
	}
}

func Load(root string) (*Index, error) {
	loaded, err := LoadCAS(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	index := New(root)
	for _, entry := range loaded.Entries {
		if err := index.Put(entry.Metadata, entry.Series); err != nil {
			return nil, fmt.Errorf("indexfile: load: %w", err)
		}
	}
	return index, nil
}

// Rebuild walks root for series subdirectories and asks read for each one's
// metadata ref. Series whose metadata is missing on disk are silently
// skipped. Progress events are reported under the "reindex" stage.
func Rebuild(ctx context.Context, root string, read func(context.Context, refs.Series) (refs.Metadata, error)) (*Index, error) {
	progress.Start(ctx, "reindex", "Rebuilding library index", 0)
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
			progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, 0)
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == paths.KuraDir {
				continue
			}
			seriesRef, err := refs.ParseSeries(entry.Name())
			if err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, 0)
				return nil, err
			}
			scanned++
			progress.Update(ctx, "reindex", fmt.Sprintf("Indexing %s", seriesRef), scanned, 0)
			metadataRef, err := read(ctx, seriesRef)
			if err != nil {
				// Series with missing or unreadable metadata are skipped
				// silently here; downstream workflows that walk the
				// filesystem (e.g. List) surface the broken state with
				// an explicit error status. Failing the whole rebuild
				// over one bad series would block every other workflow.
				continue
			}
			if err := index.Put(metadataRef, seriesRef); err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, 0)
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

func (i *Index) Get(ref refs.Metadata) (refs.Series, bool, error) {
	if ref == "" {
		return refs.Series{}, false, errors.New("indexfile: metadata ref is required")
	}
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
	existing, exists := i.refs[metadataRef]
	if exists && existing != seriesRef {
		return DuplicateRefError{Ref: metadataRef, Existing: existing, Next: seriesRef}
	}
	i.refs[metadataRef] = seriesRef
	return nil
}

func (i *Index) Remove(seriesRef refs.Series) {
	for metadataRef, existing := range i.refs {
		if existing == seriesRef {
			delete(i.refs, metadataRef)
		}
	}
}

// Entries returns the in-memory entries as a sorted slice. Convenience
// for callers building a CAS-write input from the cached state.
func (i *Index) Entries() []Entry {
	out := make([]Entry, 0, len(i.refs))
	for metadataRef, seriesRef := range i.refs {
		out = append(out, Entry{Metadata: metadataRef, Series: seriesRef})
	}
	sort.Slice(out, func(a, b int) bool {
		return out[a].Metadata.String() < out[b].Metadata.String()
	})
	return out
}

// ReplaceEntries swaps the in-memory map with the given entry list.
// Used after a successful CAS write to keep the cached read view in
// sync with what was just persisted. The CAS write itself is the
// source of truth; this just refreshes the read cache.
func (i *Index) ReplaceEntries(entries []Entry) {
	next := make(map[refs.Metadata]refs.Series, len(entries))
	for _, entry := range entries {
		next[entry.Metadata] = entry.Series
	}
	i.refs = next
}

func (i *Index) Save() error {
	if err := os.MkdirAll(paths.LibraryKuraDir(i.root), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(i.refs))
	for ref := range i.refs {
		keys = append(keys, ref.String())
	}
	sort.Strings(keys)

	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	writer.Comma = '\t'
	for _, key := range keys {
		if err := writer.Write([]string{key, i.refs[refs.Metadata(key)].String()}); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	return renameio.WriteFile(paths.IndexFile(i.root), data.Bytes(), 0o644)
}
