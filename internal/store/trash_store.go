package store

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	layout "github.com/wyvernzora/kura/internal/fsroot"
)

func (Repo) NewTrash(dirname string) (*Trash, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}
	return &Trash{
		SchemaVersion: TrashSchemaVersion,
		dirname:       dirname,
	}, nil
}

func (repo Repo) LoadTrash(dirname string) (*Trash, error) {
	dirname, err := cleanDirname(dirname)
	if err != nil {
		return nil, err
	}

	path := TrashPath(dirname)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return repo.NewTrash(dirname)
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

func (Repo) SaveTrash(trash Trash) error {
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

	return atomicWrite(metaDir, TrashPath(trash.dirname), ".trash-*.tmp", func(w io.Writer) error {
		return encodeTrash(w, trash)
	})
}
