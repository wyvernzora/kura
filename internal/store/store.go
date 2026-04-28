package store

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
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
		ID:            ulid.Make().String(),
		dirname:       dirname,
	}, nil
}

func LoadSeries(dirname string) (*Series, error) {
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
	return atomicWrite(metaDir, SeriesPath(series.dirname), ".series-*.tmp", func(w io.Writer) error {
		return encodeSeries(w, series)
	})
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
	return atomicWrite(metaDir, StagedPath(staged.dirname), ".staged-*.tmp", func(w io.Writer) error {
		return encodeStaged(w, staged)
	})
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
	return atomicWrite(metaDir, TrashPath(trash.dirname), ".trash-*.tmp", func(w io.Writer) error {
		return encodeTrash(w, trash)
	})
}

// atomicWrite stages the encoded payload through a tempfile in dir, fsyncs it,
// renames into place, and fsyncs the parent directory. Permission errors on
// the directory fsync are swallowed because some filesystems reject it for
// non-owners while still honoring the rename.
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
	return fsroot.SyncDir(dir)
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
