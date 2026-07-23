package trashfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library/internal/storage/trashfile"
)

func TestWriteReadList(t *testing.T) {
	root := t.TempDir()
	seriesRef, err := refs.ParseSeries("Honzuki")
	if err != nil {
		t.Fatal(err)
	}
	episodeRef, _ := refs.NewEpisode(1, 1)
	id := ulid.Make()
	now := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	in := trashfile.Meta{
		ID:        id,
		Episode:   episodeRef,
		TrashedAt: now,
		Record: trashfile.Record{
			Path:       "Season 1/Honzuki - S01E01.mkv",
			Source:     "BDRip",
			Resolution: "1080p",
			Size:       123,
			MTime:      now,
			Companions: []trashfile.Companion{},
			Attrs:      map[string]string{"origin": "takuhai"},
		},
	}
	if err := trashfile.Write(root, seriesRef, in); err != nil {
		t.Fatal(err)
	}

	read, err := trashfile.Read(root, seriesRef, id)
	if err != nil {
		t.Fatal(err)
	}
	if read.ID != id || read.Record.Path != in.Record.Path {
		t.Fatalf("Read = %#v", read)
	}
	if read.Record.Attrs["origin"] != "takuhai" {
		t.Fatalf("Read attrs = %#v", read.Record.Attrs)
	}

	entries, err := trashfile.List(root, seriesRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != id {
		t.Fatalf("List = %#v", entries)
	}
}

func TestDeleteRemovesEntryAndReportsBytes(t *testing.T) {
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Honzuki")
	episodeRef, _ := refs.NewEpisode(1, 1)
	id := ulid.Make()
	now := time.Now().UTC().Truncate(time.Second)
	meta := trashfile.Meta{
		ID:        id,
		Episode:   episodeRef,
		TrashedAt: now,
		Record: trashfile.Record{
			Path:       "Season 1/Honzuki - S01E01.mkv",
			Source:     "BDRip",
			Resolution: "1080p",
			Size:       7,
			MTime:      now,
			Companions: []trashfile.Companion{},
		},
	}
	if err := trashfile.Write(root, seriesRef, meta); err != nil {
		t.Fatal(err)
	}

	entryDir := paths.TrashEntry(root, seriesRef, id.String())
	mediaPath := filepath.Join(entryDir, "Honzuki - S01E01.mkv")
	if err := os.WriteFile(mediaPath, []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}

	bytes, err := trashfile.Delete(root, seriesRef, id)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if bytes == 0 {
		t.Fatal("Delete reclaimed 0 bytes; expected meta.json + media file")
	}
	if _, err := os.Stat(entryDir); !os.IsNotExist(err) {
		t.Fatalf("entry dir still present after Delete: err=%v", err)
	}

	if _, err := trashfile.Delete(root, seriesRef, id); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Delete on missing entry err=%v, want os.ErrNotExist", err)
	}
}

// TestDeleteSucceedsWithSillyRenameLeftover regresses the SMB
// silly-rename pattern: after RemoveAll runs, a hidden placeholder
// (".smbdeleteAAAxxxx" / ".fuse_hidden*" / ".nfs*") may linger inside
// the bucket because another process held an open handle on a file
// at unlink time. The retry shim must treat the bucket as logically
// empty (no meta.json, no media — kura's data is gone) and return
// success even though rmdir(bucket) would fail with ENOTEMPTY.
//
// We approximate the production scenario by chmod'ing the bucket
// dir to 555 with only a silly-rename placeholder inside: RemoveAll
// can't unlink the placeholder (no write perm on parent), so the
// retry shim re-lists, sees only silly-rename entries (realLeft==0)
// and returns nil. The placeholder remains; trashfile.List ignores
// the bucket because there's no meta.json.
func TestDeleteSucceedsWithSillyRenameLeftover(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses DAC, so chmod 0o555 doesn't block unlink — the silly-rename simulation needs an unprivileged user")
	}
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Show")
	episodeRef, _ := refs.NewEpisode(1, 1)
	id := ulid.Make()
	now := time.Now().UTC().Truncate(time.Second)
	meta := trashfile.Meta{
		ID:        id,
		Episode:   episodeRef,
		TrashedAt: now,
		Record: trashfile.Record{
			Path: "Season 1/Show - S01E01.mkv",
			Size: 1,
		},
	}
	if err := trashfile.Write(root, seriesRef, meta); err != nil {
		t.Fatal(err)
	}
	entryDir := paths.TrashEntry(root, seriesRef, id.String())
	// Strip the real children so only an SMB silly-rename
	// placeholder remains. Mirrors the moment the FS layer has
	// just renamed the still-open file aside.
	if err := os.Remove(filepath.Join(entryDir, "meta.json")); err != nil {
		t.Fatal(err)
	}
	silly := filepath.Join(entryDir, ".smbdeleteAAA0001")
	if err := os.WriteFile(silly, []byte("silly"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Lock the bucket so subsequent unlink attempts on the silly-
	// rename file return EACCES — the test stand-in for SMB EBUSY
	// on the real placeholder.
	if err := os.Chmod(entryDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(entryDir, 0o755) })

	if _, err := trashfile.Delete(root, seriesRef, id); err != nil {
		t.Fatalf("Delete: silly-rename leftover should be tolerated, got %v", err)
	}
	// Bucket dir lingers (RemoveAll couldn't rmdir it because of
	// the placeholder). That's expected — the FS layer owns the
	// placeholder now.
	if _, err := os.Stat(entryDir); err != nil {
		t.Fatalf("bucket dir gone unexpectedly: %v (silly-rename branch should leave it)", err)
	}
}
