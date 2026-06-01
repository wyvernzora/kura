//go:build !unix

package fsop

import (
	"os"
)

func normalizeMovedFile(path string, expected os.FileInfo) error {
	return os.Chmod(path, movedFileMode(expected.Mode().Perm()))
}
