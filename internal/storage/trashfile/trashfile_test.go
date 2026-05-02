package trashfile_test

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
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
