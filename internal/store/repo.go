package store

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
	layout "github.com/wyvernzora/kura/internal/fsroot"
)

type Repo struct{}

func NewRepo() Repo {
	return Repo{}
}

// New creates an unsaved series model bound to dirname.
//
// Metadata-derived fields are intentionally left for the caller to populate
// before Save. Save performs full schema validation.
func (Repo) NewSeries(dirname string) (*Series, error) {
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
func (Repo) LoadSeries(dirname string) (*Series, error) {
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
func (Repo) SaveSeries(series Series) error {
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

	return atomicWrite(metaDir, SeriesPath(series.dirname), ".series-*.tmp", func(w io.Writer) error {
		return encodeSeries(w, series)
	})
}

func atomicWrite(dir string, finalPath string, tmpPattern string, encode func(io.Writer) error) error {
	tmp, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := encode(tmp); err != nil {
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
	if err := os.Rename(tmpName, finalPath); err != nil {
		return err
	}
	return layout.SyncDir(dir)
}

func cleanDirname(dirname string) (string, error) {
	dirname = strings.TrimSpace(dirname)
	if dirname == "" {
		return "", errors.New("library: dirname is required")
	}
	return filepath.Clean(dirname), nil
}
