package library

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
	"github.com/wyvernzora/kura/internal/refs"
)

var ErrNotFound = errors.New("library index: not found")

const (
	KuraDir       = ".kura"
	IndexFileName = "index.tsv"
)

func IndexMetadataPath(libraryRoot string) string {
	return filepath.Join(libraryRoot, KuraDir, IndexFileName)
}

type DuplicateRefError struct {
	Ref      refs.Metadata
	Existing refs.Series
	Next     refs.Series
}

func (e DuplicateRefError) Error() string {
	return fmt.Sprintf("library index: %s is already tracked at %q", e.Ref, e.Existing)
}

type Index struct {
	root Root
	refs map[refs.Metadata]refs.Series
}

func NewIndex(root Root) *Index {
	return &Index{
		root: root,
		refs: map[refs.Metadata]refs.Series{},
	}
}

func LoadIndex(root Root) (*Index, error) {
	path := IndexMetadataPath(root.Path())
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	index := NewIndex(root)
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 2
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
		metadataRef, err := refs.ParseMetadata(record[0])
		if err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
		seriesRef, err := refs.ParseSeries(record[1])
		if err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
		if err := index.Put(metadataRef, seriesRef); err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
	}
	return index, nil
}

func RebuildIndex(ctx context.Context, root Root, read func(context.Context, refs.Series) (refs.Metadata, error)) (*Index, error) {
	dir, err := os.Open(root.Path())
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	index := NewIndex(root)
	for {
		entries, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == KuraDir {
				continue
			}
			seriesRef, err := refs.ParseSeries(entry.Name())
			if err != nil {
				return nil, err
			}
			metadataRef, err := read(ctx, seriesRef)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err := index.Put(metadataRef, seriesRef); err != nil {
				return nil, err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return index, nil
}

func (i *Index) Get(ref refs.Metadata) (refs.Series, bool, error) {
	if ref == "" {
		return refs.Series{}, false, errors.New("library index: metadata ref is required")
	}
	seriesRef, ok := i.refs[ref]
	return seriesRef, ok, nil
}

func (i *Index) Put(metadataRef refs.Metadata, seriesRef refs.Series) error {
	if metadataRef == "" {
		return errors.New("library index: metadata ref is required")
	}
	if seriesRef.IsZero() {
		return errors.New("library index: series ref is required")
	}
	existing, exists := i.refs[metadataRef]
	if exists && existing != seriesRef {
		return DuplicateRefError{Ref: metadataRef, Existing: existing, Next: seriesRef}
	}
	i.refs[metadataRef] = seriesRef
	return nil
}

func (i *Index) Save() error {
	if err := os.MkdirAll(filepath.Join(i.root.Path(), KuraDir), 0o755); err != nil {
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
	return renameio.WriteFile(IndexMetadataPath(i.root.Path()), data.Bytes(), 0o644)
}
