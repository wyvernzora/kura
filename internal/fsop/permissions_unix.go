//go:build unix

package fsop

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func normalizeMovedFile(path string, expected os.FileInfo) error {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(expected, info) {
		return fmt.Errorf("destination changed while normalizing %q", path)
	}
	parentGID, err := gid(filepath.Dir(path))
	if err != nil {
		return err
	}
	fileGID := gidFromInfo(info)
	if fileGID != parentGID {
		if err := file.Chown(-1, parentGID); err != nil {
			return err
		}
	}
	return file.Chmod(movedFileMode(expected.Mode().Perm()))
}

func gid(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return gidFromInfo(info), nil
}

func gidFromInfo(info os.FileInfo) int {
	return int(info.Sys().(*syscall.Stat_t).Gid)
}
