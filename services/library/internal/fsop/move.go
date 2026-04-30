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

// SafeMoveFile renames from to to, falling back to a copy-then-remove when
// the source and destination live on different filesystems. Preserves mode
// and mtime so callers (notably scan) can keep using mtime/size as a
// "unchanged" signal across reconciles.
//
// Returns an error if to already exists — callers must not rely on silent
// overwrite semantics.
func SafeMoveFile(from, to string) error {
	if from == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("fsop: target %q already exists", to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return syncParent(to)
	} else if !isCrossDeviceMove(err) {
		return err
	}
	return copyThenRemove(from, to)
}

func copyThenRemove(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("fsop: cannot move directory %q as file", from)
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
	if err := syncParent(to); err != nil {
		_ = os.Remove(to)
		return err
	}
	if err := os.Remove(from); err != nil {
		return err
	}
	return syncParent(from)
}

func syncParent(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	err = dir.Sync()
	_ = dir.Close()
	return err
}

func isCrossDeviceMove(err error) bool {
	linkErr, ok := errors.AsType[*os.LinkError](err)
	if !ok {
		return false
	}
	return errors.Is(linkErr.Err, syscall.EXDEV)
}
