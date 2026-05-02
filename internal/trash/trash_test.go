package trash

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/refs"
)

func TestWriteList(t *testing.T) {
	root := t.TempDir()
	seriesRef, err := refs.ParseSeries("Honzuki")
	if err != nil {
		t.Fatal(err)
	}
	episodeRef, _ := refs.NewEpisode(1, 1)
	id := ulid.Make()
	now := time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	if err := Write(root, seriesRef, Meta{
		ID:        id,
		Episode:   episodeRef,
		TrashedAt: now,
		Record: Record{
			Path:       "Season 1/Honzuki - S01E01.mkv",
			Source:     "BDRip",
			Resolution: "1080p",
			Size:       123,
			MTime:      now,
			Companions: []Companion{},
		},
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root, seriesRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != id {
		t.Fatalf("entries = %#v", entries)
	}
}
