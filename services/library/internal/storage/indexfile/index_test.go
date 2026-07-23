package indexfile_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/progress"
	"github.com/wyvernzora/kura/services/library/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
)

func TestIndexSaveLoad(t *testing.T) {
	root := t.TempDir()
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	model := minimalModel(t, "Honzuki", refs.Metadata("tvdb:370070"))
	if err := idx.SaveModel(context.Background(), model, coord.NewMutator("test")); err != nil {
		t.Fatal(err)
	}
	loaded, err := indexfile.Load(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := loaded.Get(refs.Metadata("tvdb:370070"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != model.Ref {
		t.Fatalf("index get = %q, %v", got, ok)
	}
	row, ok := loaded.GetRow(model.Ref)
	if !ok || row.Title != "Honzuki" || row.Metadata != model.Metadata {
		t.Fatalf("GetRow = (%+v, %v)", row, ok)
	}
}

func TestIndexSaveModelStripsMediaAttrs(t *testing.T) {
	root := t.TempDir()
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	model := minimalModel(t, "Honzuki", refs.Metadata("tvdb:370070"))
	ep, _ := refs.NewEpisode(1, 1)
	model.Episodes[ep] = series.Episode{
		Active: &media.Record{
			Path:       "/library/Honzuki/Season 1/e1.mkv",
			Source:     media.SourceBluRay,
			Companions: []media.Companion{},
			Attrs:      media.Attrs{"origin": "takuhai"},
		},
	}
	if err := idx.SaveModel(context.Background(), model, coord.NewMutator("test")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.IndexFile(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"attrs"`) || strings.Contains(string(data), "takuhai") {
		t.Fatalf("index snapshot leaked attrs:\n%s", data)
	}
	if model.Episodes[ep].Active.Attrs["origin"] != "takuhai" {
		t.Fatalf("SaveModel mutated caller attrs: %#v", model.Episodes[ep].Active.Attrs)
	}
}

func TestIndexRejectsDuplicateMetadataRef(t *testing.T) {
	idx := indexfile.New(t.TempDir(), indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	if err := idx.Upsert(indexfile.Entry{Model: minimalModel(t, "A", refs.Metadata("tvdb:370070"))}); err != nil {
		t.Fatal(err)
	}
	err := idx.Upsert(indexfile.Entry{Model: minimalModel(t, "B", refs.Metadata("tvdb:370070"))})
	var dup indexfile.DuplicateRefError
	if !errors.As(err, &dup) {
		t.Fatalf("Upsert duplicate err = %v, want DuplicateRefError", err)
	}
}

func TestIndexRemove(t *testing.T) {
	idx := indexfile.New(t.TempDir(), indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	bookworm := minimalModel(t, "Bookworm", refs.Metadata("tvdb:370070"))
	other := minimalModel(t, "Other", refs.Metadata("tvdb:111111"))
	if err := idx.Upsert(indexfile.Entry{Model: bookworm}); err != nil {
		t.Fatal(err)
	}
	if err := idx.Upsert(indexfile.Entry{Model: other}); err != nil {
		t.Fatal(err)
	}
	idx.Remove(bookworm.Ref)
	if _, ok, err := idx.Get(bookworm.Metadata); err != nil || ok {
		t.Fatalf("Get old ref = _, %v, %v; want absent", ok, err)
	}
	if _, ok := idx.GetRow(bookworm.Ref); ok {
		t.Fatal("GetRow after Remove should be absent")
	}
	if got, ok, err := idx.Get(other.Metadata); err != nil || !ok || got != other.Ref {
		t.Fatalf("Get other = %q, %v, %v; want %q, true, nil", got, ok, err, other.Ref)
	}
}

func TestRebuildReportsProgress(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}
	var events []progress.Event
	ctx := progress.With(context.Background(), func(_ context.Context, event progress.Event) {
		events = append(events, event)
	})
	idx := indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	idx.SetEntryBuilderForTest(func(context.Context, string, refs.Series) (indexfile.Entry, error) {
		return indexfile.Entry{Model: minimalModel(t, "Bookworm", refs.Metadata("tvdb:370070"))}, nil
	})
	if err := idx.RebuildNow(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	if len(events) < 3 {
		t.Fatalf("events = %#v, want start/update/success", events)
	}
	if events[0].Status != progress.StartStatus || events[0].Stage != "reindex" {
		t.Fatalf("first event = %#v, want reindex start", events[0])
	}
	if events[len(events)-1].Status != progress.SuccessStatus || events[len(events)-1].Stage != "reindex" {
		t.Fatalf("last event = %#v, want reindex success", events[len(events)-1])
	}
}
