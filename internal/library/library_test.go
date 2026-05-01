package library

import (
	"context"
	"os"
	"testing"

	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
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
	series, err := handle.Read(context.Background(), seriespkg.ReadInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(series.Seasons) != 1 || len(series.Seasons[0].Episodes) != 2 {
		t.Fatalf("series = %#v, want 2 episodes", series)
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

func TestLibraryImportForceReplacesSeriesJSONAndPreservesKuraSiblings(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref := mustSeries(t, "Bookworm")
	seriesDir := root.Join("Bookworm")
	if err := os.MkdirAll(root.Join("Bookworm", ".kura", "trash", "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root.Join("Bookworm", ".kura", "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.Join("Bookworm", ".kura", "series.json"), []byte(`{"schemaVersion":1,"metadataRef":"tvdb:999999","episodes":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.Join("Bookworm", ".kura", "trash", "old", "meta.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.Join("Bookworm", ".kura", "logs", "old.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewIndex(root)
	if err := idx.Put(refs.Metadata("tvdb:999999"), ref); err != nil {
		t.Fatal(err)
	}
	lib := New(root, fakeSource{}, mediainfo.Inspector{}, idx)
	if _, err := lib.Import(context.Background(), ImportInput{Metadata: refs.Metadata("tvdb:370070"), Ref: ref, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root.Join("Bookworm", ".kura", "trash", "old", "meta.json")); err != nil {
		t.Fatalf("trash meta was not preserved: %v", err)
	}
	if _, err := os.Stat(root.Join("Bookworm", ".kura", "logs", "old.jsonl")); err != nil {
		t.Fatalf("log was not preserved: %v", err)
	}
	if _, ok, err := idx.Get(refs.Metadata("tvdb:999999")); err != nil || ok {
		t.Fatalf("old index ref = _, %v, %v; want absent", ok, err)
	}
	if got, ok, err := idx.Get(refs.Metadata("tvdb:370070")); err != nil || !ok || got != ref {
		t.Fatalf("new index ref = %q, %v, %v; want %q, true, nil", got, ok, err, ref)
	}
	handle, err := lib.Find(refs.Metadata("tvdb:370070"))
	if err != nil {
		t.Fatal(err)
	}
	view, err := handle.Read(context.Background(), seriespkg.ReadInput{})
	if err != nil {
		t.Fatal(err)
	}
	if view.MetadataRef != refs.Metadata("tvdb:370070") || len(view.Seasons[0].Episodes) != 2 {
		t.Fatalf("view = %#v, want fresh imported metadata", view)
	}
	if _, err := os.Stat(seriesDir); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryImportReportsProgress(t *testing.T) {
	root, err := ParseRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root.Join("Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}
	var events []progress.Event
	ctx := progress.With(context.Background(), func(_ context.Context, event progress.Event) {
		events = append(events, event)
	})
	lib := New(root, fakeSource{}, mediainfo.Inspector{}, NewIndex(root))
	if _, err := lib.Import(ctx, ImportInput{Metadata: refs.Metadata("tvdb:370070"), Ref: mustSeries(t, "Bookworm")}); err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("events = %#v, want start/update/success", events)
	}
	if events[0].Status != progress.StartStatus || events[0].Stage != "import" {
		t.Fatalf("first event = %#v, want import start", events[0])
	}
	if events[len(events)-1].Status != progress.SuccessStatus || events[len(events)-1].Stage != "import" {
		t.Fatalf("last event = %#v, want import success", events[len(events)-1])
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
