package library

import (
	"context"
	"os"
	"testing"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/index"
	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/refs"
)

func TestLibraryAddWritesFullSpine(t *testing.T) {
	root, err := fsroot.ParseLibraryRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib := New(root, fakeSource{}, mediainfo.Inspector{}, index.New(root))
	handle, err := lib.Add(context.Background(), AddInput{Metadata: refs.Metadata("tvdb:370070"), Ref: refs.Series("Bookworm")})
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
	root, err := fsroot.ParseLibraryRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib := New(root, fakeSource{}, mediainfo.Inspector{}, index.New(root))
	if _, err := lib.Import(context.Background(), ImportInput{Metadata: refs.Metadata("tvdb:370070"), Ref: refs.Series("Missing")}); err == nil {
		t.Fatal("expected missing series error")
	}
	if err := os.Mkdir(root.Join("Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Import(context.Background(), ImportInput{Metadata: refs.Metadata("tvdb:370070"), Ref: refs.Series("Bookworm")}); err != nil {
		t.Fatal(err)
	}
}

type fakeSource struct{}

func (fakeSource) Key() string { return "tvdb" }

func (fakeSource) Search(context.Context, string, metadata.SearchOptions) ([]metadata.SearchResult, error) {
	return nil, nil
}

func (fakeSource) GetSeries(context.Context, string) (metadata.Series, error) {
	return metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			MetadataRef:    refs.Metadata("tvdb:370070"),
			PreferredTitle: "Bookworm",
		},
		Seasons: []metadata.Season{
			{
				Number: 1,
				Episodes: []metadata.Episode{
					{SeasonNumber: 1, EpisodeNumber: 1, Aired: "2019-10-02"},
					{SeasonNumber: 1, EpisodeNumber: 2, Aired: "2019-10-09"},
				},
			},
		},
	}, nil
}
