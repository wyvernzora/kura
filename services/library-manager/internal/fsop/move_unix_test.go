//go:build unix

package fsop_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/fsop"
)

func TestSafeMoveFileAlignsGroupWithParent(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	from := filepath.Join(dir, "from.mkv")
	if err := os.WriteFile(from, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourceGID, err := testGID(from)
	if err != nil {
		t.Fatal(err)
	}

	parentGID := -1
	for _, gid := range groups {
		if gid != sourceGID {
			parentGID = gid
			break
		}
	}
	if parentGID < 0 {
		t.Skip("no secondary group available for group-alignment test")
	}

	toDir := filepath.Join(dir, "series", "Season 1")
	if err := os.MkdirAll(toDir, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(toDir, -1, parentGID); err != nil {
		t.Skipf("cannot chgrp destination parent to secondary group: %v", err)
	}

	to := filepath.Join(toDir, "to.mkv")
	if err := fsop.SafeMoveFile(from, to); err != nil {
		t.Fatalf("SafeMoveFile: %v", err)
	}
	got, err := testGID(to)
	if err != nil {
		t.Fatal(err)
	}
	if got != parentGID {
		t.Fatalf("dest gid = %d, want parent gid %d", got, parentGID)
	}
}

func testGID(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return int(info.Sys().(*syscall.Stat_t).Gid), nil
}
