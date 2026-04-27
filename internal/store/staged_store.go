package store

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	layout "github.com/wyvernzora/kura/internal/fsroot"
)

func (Repo) NewStaged(dirname string) (*Staged, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	return &Staged{
		SchemaVersion: StagedSchemaVersion,
		dirname:       dirname,
	}, nil
}

func (repo Repo) LoadStaged(dirname string) (*Staged, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}

	path := StagedPath(dirname)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return repo.NewStaged(dirname)
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

func (Repo) SaveStaged(staged Staged) error {
	if staged.dirname == "" {
		return errors.New("library: staged is not bound to a directory")
	}
	if staged.IsEmpty() {
		return removeMetadataFile(StagedPath(staged.dirname))
	}
	if err := staged.Validate(); err != nil {
		return err
	}

	metaDir := filepath.Join(staged.dirname, layout.KuraDir)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}

	return atomicWrite(metaDir, StagedPath(staged.dirname), ".staged-*.tmp", func(w io.Writer) error {
		return encodeStaged(w, staged)
	})
}
