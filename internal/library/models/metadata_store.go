package models

import (
	"errors"
	"os"
	"path/filepath"
)

func removeMetadataFile(path string) error {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return syncDir(filepath.Dir(path))
}
