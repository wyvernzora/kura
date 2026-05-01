package library

import (
	"context"
	"os"
	"testing"

	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func TestLibraryAddWritesFullSpine(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib := New(root, fakeSource{}, mediainfo.Inspector{}, NewIndex(root))
	handle, err := lib.Add(context.Background(), AddInput{Metadata: refs.Metadata("tvdb:370070"), Ref: mustSeries(t, "Bookworm")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root.Join("Bookworm", ".kura", "series.json")); err != nil {
		t.Fatal(err)
	}
	series, err := handle.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Episodes) != 2 {
		t.Fatalf("episodes = %d, want 2", len(series.Episodes))
	}
}

func TestLibraryImportRequiresExistingUntrackedDir(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib := New(root, fakeSource{}, mediainfo.Inspector{}, NewIndex(root))
	if _, err := lib.Import(context.Background(), ImportInput{Metadata: refs.Metadata("tvdb:370070"), Ref: mustSeries(t, "Missing")}); err == nil {
		t.Fatal("expected missing series error")
	}
	if err := os.Mkdir(root.Join("Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Import(context.Background(), ImportInput{Metadata: refs.Metadata("tvdb:370070"), Ref: mustSeries(t, "Bookworm")}); err != nil {
		t.Fatal(err)
	}
}

type fakeSource struct{}

func (fakeSource) Key() string { return "tvdb" }

func (fakeSource) Search(context.Context, textnorm.NFCString, metadata.SearchOptions) ([]metadata.SearchResult, error) {
	return nil, nil
}

func (fakeSource) GetSeries(context.Context, string) (metadata.Series, error) {
	episodeOne, _ := refs.NewEpisode(1, 1)
	episodeTwo, _ := refs.NewEpisode(1, 2)
	return metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			MetadataRef:    refs.Metadata("tvdb:370070"),
			PreferredTitle: textnorm.NFC("Bookworm"),
		},
		Seasons: []metadata.Season{
			{
				Number: 1,
				Episodes: []metadata.Episode{
					{Ref: episodeOne, Aired: "2019-10-02"},
					{Ref: episodeTwo, Aired: "2019-10-09"},
				},
			},
		},
	}, nil
}
