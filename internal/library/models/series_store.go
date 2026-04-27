package models

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/library/layout"
)

type Store struct{}

func NewStore() Store {
	return Store{}
}

// New creates an unsaved series model bound to dirname.
//
// Metadata-derived fields are intentionally left for the caller to populate
// before Save. Save performs full schema validation.
func (Store) New(dirname string) (*Series, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}

	return &Series{
		SchemaVersion: SeriesSchemaVersion,
		ID:            ulid.Make().String(),
		dirname:       dirname,
	}, nil
}

// Load reads and validates <dirname>/.kura/series.json.
func (Store) Load(dirname string) (*Series, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}

	path := SeriesPath(dirname)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	series, err := decodeSeries(data, path)
	if err != nil {
		return nil, err
	}
	series.dirname = dirname
	if err := series.Validate(); err != nil {
		return nil, err
	}
	return &series, nil
}

// Save atomically writes series to its bound <dirname>/.kura/series.json path.
func (Store) Save(series Series) error {
	if series.dirname == "" {
		return errors.New("library: series is not bound to a directory")
	}
	if err := series.Validate(); err != nil {
		return err
	}

	metaDir := filepath.Join(series.dirname, layout.KuraDir)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}

	path := SeriesPath(series.dirname)
	tmp, err := os.CreateTemp(metaDir, ".series-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := encodeSeries(tmp, series); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(metaDir)
}

func cleanDirname(dirname string) (string, error) {
	dirname = strings.TrimSpace(dirname)
	if dirname == "" {
		return "", errors.New("library: dirname is required")
	}
	return filepath.Clean(dirname), nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil
		}
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
