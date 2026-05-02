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
// Caller is responsible for ensuring to does not already exist; copy-mode
// uses O_EXCL so an existing target surfaces as an error rather than a
// silent overwrite.
func SafeMoveFile(from, to string) error {
	if from == to {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
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
	if err := dst.Close(); err != nil {
		_ = os.Remove(to)
		return err
	}
	if err := os.Chtimes(to, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	return os.Remove(from)
}

func isCrossDeviceMove(err error) bool {
	linkErr, ok := errors.AsType[*os.LinkError](err)
	if !ok {
		return false
	}
	return errors.Is(linkErr.Err, syscall.EXDEV)
}
