package trash

import (
	"os"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/refs"
)

func TestWriteList(t *testing.T) {
	root, err := fsroot.ParseLibraryRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seriesRef := refs.Series("Honzuki")
	if err := os.Mkdir(root.Join(seriesRef.String()), 0o755); err != nil {
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
