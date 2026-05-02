package fsop_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/fsop"
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

func TestSafeMoveFileRenamesAndPreservesMtime(t *testing.T) {
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

func TestSafeMoveFileMissingSource(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "missing.mkv")
	to := filepath.Join(dir, "dest.mkv")
	if err := fsop.SafeMoveFile(from, to); err == nil {
		t.Fatal("SafeMoveFile: nil err for missing source")
	}
}
