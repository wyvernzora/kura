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
	"path/filepath"
	"sort"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
)

var ErrNotFound = errors.New("indexfile: not found")

const (
	kuraDir       = ".kura"
	indexFileName = "index.tsv"
)

// MetadataPath returns the absolute path to <root>/.kura/index.tsv.
func MetadataPath(root string) string {
	return filepath.Join(root, kuraDir, indexFileName)
}

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
	path := MetadataPath(root)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	index := New(root)
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 2
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("indexfile: read %s: %w", path, err)
		}
		metadataRef, err := refs.ParseMetadata(record[0])
		if err != nil {
			return nil, fmt.Errorf("indexfile: read %s: %w", path, err)
		}
		seriesRef, err := refs.ParseSeries(record[1])
		if err != nil {
			return nil, fmt.Errorf("indexfile: read %s: %w", path, err)
		}
		if err := index.Put(metadataRef, seriesRef); err != nil {
			return nil, fmt.Errorf("indexfile: read %s: %w", path, err)
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
			if !entry.IsDir() || entry.Name() == kuraDir {
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
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				progress.Failure(ctx, "reindex", "Failed to rebuild library index", scanned, 0)
				return nil, err
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

func (i *Index) Save() error {
	if err := os.MkdirAll(filepath.Join(i.root, kuraDir), 0o755); err != nil {
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
	return renameio.WriteFile(MetadataPath(i.root), data.Bytes(), 0o644)
}
