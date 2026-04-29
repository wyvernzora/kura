package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/renameio/v2"
	"github.com/wyvernzora/kura/internal/fsroot"
)

// NewSeries creates an unsaved series model bound to dirname.
//
// Metadata-derived fields are intentionally left for the caller to populate
// before Save. SaveSeries performs full schema validation.
func NewSeries(dirname string) (*Series, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	return &Series{
		SchemaVersion: SeriesSchemaVersion,
		dirname:       dirname,
	}, nil
}

func LoadSeries(dirname string) (*Series, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	path := SeriesMetadataPath(dirname)
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

func SaveSeries(series Series) error {
	if series.dirname == "" {
		return errors.New("library: series is not bound to a directory")
	}
	if err := series.Validate(); err != nil {
		return err
	}
	metaDir := filepath.Join(series.dirname, fsroot.KuraDir)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	var data bytes.Buffer
	if err := encodeSeries(&data, series); err != nil {
		return err
	}
	return renameio.WriteFile(SeriesMetadataPath(series.dirname), data.Bytes(), 0o644)
}

func NewStaged(dirname string) (*Staged, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	return &Staged{
		SchemaVersion: StagedSchemaVersion,
		dirname:       dirname,
	}, nil
}

func LoadStaged(dirname string) (*Staged, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	path := StagedPath(dirname)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewStaged(dirname)
	}
	if err != nil {
		return nil, err
	}
	staged, err := decodeStaged(data, path)
	if err != nil {
		return nil, err
	}
	staged.dirname = dirname
	if err := staged.Validate(); err != nil {
		return nil, err
	}
	return &staged, nil
}

func SaveStaged(staged Staged) error {
	if staged.dirname == "" {
		return errors.New("library: staged is not bound to a directory")
	}
	if staged.IsEmpty() {
		return removeMetadataFile(StagedPath(staged.dirname))
	}
	if err := staged.Validate(); err != nil {
		return err
	}
	metaDir := filepath.Join(staged.dirname, fsroot.KuraDir)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	var data bytes.Buffer
	if err := encodeStaged(&data, staged); err != nil {
		return err
	}
	return renameio.WriteFile(StagedPath(staged.dirname), data.Bytes(), 0o644)
}

func NewTrash(dirname string) (*Trash, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	return &Trash{
		SchemaVersion: TrashSchemaVersion,
		dirname:       dirname,
	}, nil
}

func LoadTrash(dirname string) (*Trash, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	path := TrashPath(dirname)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewTrash(dirname)
	}
	if err != nil {
		return nil, err
	}
	trash, err := decodeTrash(data, path)
	if err != nil {
		return nil, err
	}
	trash.dirname = dirname
	if err := trash.Validate(); err != nil {
		return nil, err
	}
	return &trash, nil
}

func SaveTrash(trash Trash) error {
	if trash.dirname == "" {
		return errors.New("library: trash is not bound to a directory")
	}
	if trash.IsEmpty() {
		return removeMetadataFile(TrashPath(trash.dirname))
	}
	if err := trash.Validate(); err != nil {
		return err
	}
	metaDir := filepath.Join(trash.dirname, fsroot.KuraDir)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	var data bytes.Buffer
	if err := encodeTrash(&data, trash); err != nil {
		return err
	}
	return renameio.WriteFile(TrashPath(trash.dirname), data.Bytes(), 0o644)
}

func removeMetadataFile(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return fsroot.SyncDir(filepath.Dir(path))
}

func cleanDirname(dirname string) (string, error) {
	dirname = strings.TrimSpace(dirname)
	if dirname == "" {
		return "", errors.New("library: dirname is required")
	}
	return filepath.Clean(dirname), nil
}
