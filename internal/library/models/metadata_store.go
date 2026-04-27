package models

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/fsroot"
)

func removeMetadataFile(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return fsroot.SyncDir(filepath.Dir(path))
}
