package store

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
	"sync"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/progress"
)

var (
	ErrLibraryIndexNotFound = errors.New("library index: not found")
	ErrMissingLibraryIndex  = errors.New("no library index on context")
)

type DuplicateLibraryIndexRefError struct {
	Ref      domain.MetadataRef
	Existing domain.SeriesPath
	Next     domain.SeriesPath
}

func (e DuplicateLibraryIndexRefError) Error() string {
	return fmt.Sprintf("library index: %s is already tracked at %q", e.Ref, e.Existing)
}

// LibraryIndex maps metadata refs to direct child series directories.
type LibraryIndex struct {
	root fsroot.LibraryRoot
	refs map[string]domain.SeriesPath
}

func NewLibraryIndex(root fsroot.LibraryRoot) *LibraryIndex {
	return &LibraryIndex{
		root: root,
		refs: map[string]domain.SeriesPath{},
	}
}

func LoadLibraryIndex(root fsroot.LibraryRoot) (*LibraryIndex, error) {
	path := fsroot.IndexMetadataPath(root.Path())
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrLibraryIndexNotFound
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	index := NewLibraryIndex(root)
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.FieldsPerRecord = 2
	reader.TrimLeadingSpace = false
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
		ref, err := domain.ParseMetadataRef(record[0])
		if err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
		seriesPath, err := domain.ParseSeriesPath(record[1])
		if err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
		if err := index.putRef(ref, seriesPath); err != nil {
			return nil, fmt.Errorf("library index: read %s: %w", path, err)
		}
	}
	return index, nil
}

func RebuildLibraryIndex(ctx context.Context, root fsroot.LibraryRoot) (*LibraryIndex, error) {
	progress.Start(ctx, "library-index", "Scanning library index", 0)
	dir, err := os.Open(root.Path())
	if err != nil {
		progress.Failure(ctx, "library-index", "Failed scanning library index", 0, 0)
		return nil, err
	}
	defer dir.Close()

	index := NewLibraryIndex(root)
	scanned := 0
	indexed := 0
	for {
		entries, err := dir.ReadDir(64)
		if err != nil && !errors.Is(err, io.EOF) {
			progress.Failure(ctx, "library-index", "Failed scanning library index", scanned, 0)
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == fsroot.KuraDir {
				continue
			}
			scanned++
			progress.Update(ctx, "library-index", fmt.Sprintf("Indexing %d candidate series directories", scanned), scanned, 0)
			seriesDir := root.Join(entry.Name())
			series, loadErr := LoadSeries(seriesDir)
			if errors.Is(loadErr, os.ErrNotExist) {
				continue
			}
			if loadErr != nil {
				progress.Failure(ctx, "library-index", fmt.Sprintf("Failed indexing %s", entry.Name()), scanned, 0)
				return nil, loadErr
			}
			seriesPath, parseErr := domain.ParseSeriesPath(entry.Name())
			if parseErr != nil {
				progress.Failure(ctx, "library-index", fmt.Sprintf("Failed indexing %s", entry.Name()), scanned, 0)
				return nil, parseErr
			}
			if putErr := index.Put(*series, seriesPath); putErr != nil {
				progress.Failure(ctx, "library-index", fmt.Sprintf("Failed indexing %s", entry.Name()), scanned, 0)
				return nil, putErr
			}
			indexed++
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if err := index.Save(); err != nil {
		progress.Failure(ctx, "library-index", "Failed writing library index", scanned, 0)
		return nil, err
	}
	progress.Success(ctx, "library-index", fmt.Sprintf("Indexed %d tracked series", indexed), scanned)
	return index, nil
}

func (i *LibraryIndex) Get(ref domain.MetadataRef) (domain.SeriesPath, bool, error) {
	if ref.IsZero() {
		return domain.SeriesPath{}, false, errors.New("library index: metadata ref is required")
	}
	path, ok := i.refs[ref.String()]
	return path, ok, nil
}

func (i *LibraryIndex) Put(series Series, path domain.SeriesPath) error {
	if path.IsZero() {
		return errors.New("library index: series path is required")
	}
	ref, err := domain.ParseMetadataRef(series.MetadataRef)
	if err != nil {
		return err
	}
	return i.putRef(ref, path)
}

func (i *LibraryIndex) putRef(ref domain.MetadataRef, path domain.SeriesPath) error {
	key := ref.String()
	existing, exists := i.refs[key]
	if exists && existing.String() != path.String() {
		return DuplicateLibraryIndexRefError{Ref: ref, Existing: existing, Next: path}
	}
	i.refs[key] = path
	return nil
}

func (i *LibraryIndex) Save() error {
	if err := os.MkdirAll(filepath.Join(i.root.Path(), fsroot.KuraDir), 0o755); err != nil {
		return err
	}
	var refs []string
	for ref := range i.refs {
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	writer.Comma = '\t'
	for _, ref := range refs {
		if err := writer.Write([]string{ref, i.refs[ref].String()}); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	path := fsroot.IndexMetadataPath(i.root.Path())
	if err := renameio.WriteFile(path, data.Bytes(), 0o644); err != nil {
		return err
	}
	return fsroot.SyncDir(filepath.Dir(path))
}

type libraryIndexContextKey struct{}

func WithLibraryIndex(ctx context.Context, build func() (*LibraryIndex, error)) context.Context {
	var once sync.Once
	var index *LibraryIndex
	var err error
	memoized := func() (*LibraryIndex, error) {
		once.Do(func() {
			if build == nil {
				err = ErrMissingLibraryIndex
				return
			}
			index, err = build()
		})
		return index, err
	}
	return context.WithValue(ctx, libraryIndexContextKey{}, memoized)
}

func LibraryIndexFrom(ctx context.Context) (*LibraryIndex, error) {
	if ctx == nil {
		return nil, ErrMissingLibraryIndex
	}
	build, ok := ctx.Value(libraryIndexContextKey{}).(func() (*LibraryIndex, error))
	if !ok {
		return nil, ErrMissingLibraryIndex
	}
	return build()
}
