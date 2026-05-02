package trashfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/trashfile"
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
