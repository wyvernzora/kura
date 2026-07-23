package fsop_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/fsop"
)

func TestSafeMoveFileSamePathIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.mkv")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fsop.SafeMoveFile(path, path); err != nil {
		t.Fatalf("SafeMoveFile: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file missing after no-op: %v", err)
	}
}

func TestSafeMoveFileMovesAndPreservesMtime(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "from.mkv")
	to := filepath.Join(dir, "sub", "to.mkv")
	if err := os.WriteFile(from, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(from, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := fsop.SafeMoveFile(from, to); err != nil {
		t.Fatalf("SafeMoveFile: %v", err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("source still present: err=%v", err)
	}
	info, err := os.Stat(to)
	if err != nil {
		t.Fatalf("dest missing: %v", err)
	}
	if !info.ModTime().Equal(mtime) {
		t.Fatalf("mtime not preserved: got %v want %v", info.ModTime(), mtime)
	}
}

func TestSafeMoveFileAppliesConfiguredPermissionMask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not stable on windows")
	}

	tests := []struct {
		name string
		mask int
		want os.FileMode
	}{
		{name: "group writable", mask: 0o007, want: 0o660},
		{name: "group readable", mask: 0o027, want: 0o640},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restore := fsop.SetPermissionMask(tc.mask)
			t.Cleanup(restore)

			dir := t.TempDir()
			from := filepath.Join(dir, "from.mkv")
			to := filepath.Join(dir, "sub", "to.mkv")
			if err := os.WriteFile(from, []byte("body"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(from, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := fsop.SafeMoveFile(from, to); err != nil {
				t.Fatalf("SafeMoveFile: %v", err)
			}
			info, err := os.Stat(to)
			if err != nil {
				t.Fatalf("dest missing: %v", err)
			}
			if got := info.Mode().Perm(); got != tc.want {
				t.Fatalf("dest mode = %#o, want %#o", got, tc.want)
			}
		})
	}
}

func TestSafeMoveFileRejectsSymlinkSource(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.mkv")
	from := filepath.Join(dir, "from.mkv")
	to := filepath.Join(dir, "sub", "to.mkv")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, from); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := fsop.SafeMoveFile(from, to)
	if err == nil {
		t.Fatal("SafeMoveFile: nil err for symlink source")
	}
	if !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("SafeMoveFile: err = %v, want non-regular", err)
	}
	if _, err := os.Lstat(from); err != nil {
		t.Fatalf("source symlink missing after rejected move: %v", err)
	}
	if _, err := os.Lstat(to); !os.IsNotExist(err) {
		t.Fatalf("target exists after rejected move: err=%v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("symlink target missing: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("symlink target mode = %#o, want %#o", got, os.FileMode(0o600))
	}
}

func TestSafeMoveFileMissingSource(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "missing.mkv")
	to := filepath.Join(dir, "dest.mkv")
	if err := fsop.SafeMoveFile(from, to); err == nil {
		t.Fatal("SafeMoveFile: nil err for missing source")
	}
}

func TestSafeMoveFileRejectsExistingTarget(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "from.mkv")
	to := filepath.Join(dir, "to.mkv")
	if err := os.WriteFile(from, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := fsop.SafeMoveFile(from, to)
	if err == nil {
		t.Fatal("SafeMoveFile: nil err when target already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("SafeMoveFile: err = %v, want 'already exists'", err)
	}
	// Source must be untouched.
	if _, err := os.Stat(from); err != nil {
		t.Fatalf("source missing after rejected move: %v", err)
	}
	// Target must retain original content.
	got, err := os.ReadFile(to)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "existing" {
		t.Fatalf("target content changed: got %q", got)
	}
}
