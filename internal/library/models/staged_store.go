package models

import (
	"errors"
	"os"
	"path/filepath"

	layout "github.com/wyvernzora/kura/internal/fsroot"
)

func (Store) NewStaged(dirname string) (*Staged, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	return &Staged{
		SchemaVersion: StagedSchemaVersion,
		dirname:       dirname,
	}, nil
}

func (store Store) LoadStaged(dirname string) (*Staged, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}

	path := StagedPath(dirname)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store.NewStaged(dirname)
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

func (Store) SaveStaged(staged Staged) error {
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

	path := StagedPath(staged.dirname)
	tmp, err := os.CreateTemp(metaDir, ".staged-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := encodeStaged(tmp, staged); err != nil {
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
	return layout.SyncDir(metaDir)
}
