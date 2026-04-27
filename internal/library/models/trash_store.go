package models

import (
	"errors"
	"os"
	"path/filepath"

	layout "github.com/wyvernzora/kura/internal/fsroot"
)

func (Store) NewTrash(dirname string) (*Trash, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	return &Trash{
		SchemaVersion: TrashSchemaVersion,
		dirname:       dirname,
	}, nil
}

func (store Store) LoadTrash(dirname string) (*Trash, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}

	path := TrashPath(dirname)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store.NewTrash(dirname)
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

func (Store) SaveTrash(trash Trash) error {
	if trash.dirname == "" {
		return errors.New("library: trash is not bound to a directory")
	}
	if trash.IsEmpty() {
		return removeMetadataFile(TrashPath(trash.dirname))
	}
	if err := trash.Validate(); err != nil {
		return err
	}

	metaDir := filepath.Join(trash.dirname, layout.KuraDir)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}

	path := TrashPath(trash.dirname)
	tmp, err := os.CreateTemp(metaDir, ".trash-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := encodeTrash(tmp, trash); err != nil {
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
