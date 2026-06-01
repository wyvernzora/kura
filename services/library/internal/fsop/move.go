// Package fsop provides small filesystem primitives that don't fit any
// specific storage artifact: cross-device file moves, atomic renames, etc.
//
// Kept narrow on purpose. Anything growing beyond a few helpers should
// move to its own package.
package fsop

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// SafeMoveFile moves from to to, normalizes the destination file, and refuses
// to overwrite an existing target under Kura's single-writer library model.
// It uses an O(1) rename for normal same-filesystem moves and only copies
// file contents for true cross-device moves. The destination is normalized so
// Kura mirrors owner read/write bits to the group, aligns the file group with
// its parent where possible, and reduces permissions by the configured
// permission mask.
//
// Returns an error if to already exists — callers must not rely on silent
// overwrite semantics.
func SafeMoveFile(from, to string) error {
	if from == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o775); err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("fsop: target %q already exists", to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("fsop: cannot move non-regular file %q", from)
	}
	if err := os.Rename(from, to); err != nil {
		if isCrossDeviceMove(err) {
			return copyThenRemove(from, to, info)
		}
		return err
	}
	if err := normalizeMovedFile(to, info); err != nil {
		if rollbackErr := os.Rename(to, from); rollbackErr != nil {
			return fmt.Errorf("fsop: normalize moved file %q: %w (rollback failed: %v)", to, err, rollbackErr)
		}
		return fmt.Errorf("fsop: normalize moved file %q: %w", to, err)
	}
	bestEffortSyncParent(to)
	bestEffortSyncParent(from)
	return nil
}

func copyThenRemove(from, to string, info os.FileInfo) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	openedInfo, err := src.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, openedInfo) {
		return fmt.Errorf("fsop: source changed while moving %q", from)
	}
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(to)
		return err
	}
	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		_ = os.Remove(to)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(to)
		return err
	}
	// mtime preservation is a scan convenience, not a safety property.
	_ = os.Chtimes(to, info.ModTime(), info.ModTime())
	dstInfo, err := os.Lstat(to)
	if err != nil {
		_ = os.Remove(to)
		return err
	}
	if !dstInfo.Mode().IsRegular() {
		_ = os.Remove(to)
		return fmt.Errorf("fsop: destination changed while moving %q", to)
	}
	if err := normalizeMovedFile(to, dstInfo); err != nil {
		_ = os.Remove(to)
		return fmt.Errorf("fsop: normalize moved file %q: %w", to, err)
	}
	bestEffortSyncParent(to)
	if err := os.Remove(from); err != nil {
		return err
	}
	bestEffortSyncParent(from)
	return nil
}

func bestEffortSyncParent(path string) {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return
	}
	_ = dir.Sync()
	_ = dir.Close()
}

func isCrossDeviceMove(err error) bool {
	linkErr, ok := errors.AsType[*os.LinkError](err)
	if !ok {
		return false
	}
	return errors.Is(linkErr.Err, syscall.EXDEV)
}
