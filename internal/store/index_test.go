package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/progress"
)

func TestLibraryIndexSaveWritesRefsSortedByID(t *testing.T) {
	root := testIndexRoot(t)
	index := NewLibraryIndex(root)
	if err := index.Put(testIndexSeries(t, root, "B", "tvdb:2"), mustSeriesPath(t, "B")); err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if err := index.Put(testIndexSeries(t, root, "A", "tvdb:1"), mustSeriesPath(t, "A")); err != nil {
		t.Fatalf("Put A: %v", err)
	}
	if err := index.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(fsroot.IndexMetadataPath(root.Path()))
	if err != nil {
		t.Fatalf("ReadFile index: %v", err)
	}
	if got, want := string(data), "tvdb:1\tA\ntvdb:2\tB\n"; got != want {
		t.Fatalf("index.tsv = %q, want %q", got, want)
	}
}

func TestLoadLibraryIndexReturnsNotFoundWhenMissing(t *testing.T) {
	root := testIndexRoot(t)
	_, err := LoadLibraryIndex(root)
	if !errors.Is(err, ErrLibraryIndexNotFound) {
		t.Fatalf("LoadLibraryIndex error = %v, want ErrLibraryIndexNotFound", err)
	}
}

func TestLoadLibraryIndexRejectsDuplicateRefs(t *testing.T) {
	root := testIndexRoot(t)
	if err := os.MkdirAll(filepath.Join(root.Path(), fsroot.KuraDir), 0o755); err != nil {
		t.Fatalf("MkdirAll .kura: %v", err)
	}
	data := []byte("tvdb:1\tA\ntvdb:1\tB\n")
	if err := os.WriteFile(fsroot.IndexMetadataPath(root.Path()), data, 0o644); err != nil {
		t.Fatalf("WriteFile index: %v", err)
	}

	_, err := LoadLibraryIndex(root)
	var duplicate DuplicateLibraryIndexRefError
	if !errors.As(err, &duplicate) {
		t.Fatalf("LoadLibraryIndex error = %v, want DuplicateLibraryIndexRefError", err)
	}
}

func TestRebuildLibraryIndexStreamsTrackedSeriesAndIgnoresUninitialized(t *testing.T) {
	root := testIndexRoot(t)
	saveIndexSeries(t, root, "Bookworm", "tvdb:370070")
	if err := os.Mkdir(filepath.Join(root.Path(), "Untracked"), 0o755); err != nil {
		t.Fatalf("Mkdir Untracked: %v", err)
	}

	var events []progress.Event
	ctx := progress.With(context.Background(), func(_ context.Context, event progress.Event) {
		events = append(events, event)
	})
	index, err := RebuildLibraryIndex(ctx, root)
	if err != nil {
		t.Fatalf("RebuildLibraryIndex: %v", err)
	}

	ref, err := domain.ParseMetadataRef("tvdb:370070")
	if err != nil {
		t.Fatalf("ParseMetadataRef: %v", err)
	}
	path, ok, err := index.Get(ref)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || path.String() != "Bookworm" {
		t.Fatalf("Get = %q, %v; want Bookworm, true", path, ok)
	}
	if _, err := os.Stat(fsroot.IndexMetadataPath(root.Path())); err != nil {
		t.Fatalf("Stat index.tsv: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("progress events = %d, want at least start/update/success", len(events))
	}
	if events[0].Status != progress.StartStatus {
		t.Fatalf("first progress status = %s, want start", events[0].Status)
	}
	if events[len(events)-1].Status != progress.SuccessStatus {
		t.Fatalf("last progress status = %s, want success", events[len(events)-1].Status)
	}
}

func TestLibraryIndexPutFailsWhenRefAlreadyPointsElsewhere(t *testing.T) {
	root := testIndexRoot(t)
	index := NewLibraryIndex(root)
	if err := index.Put(testIndexSeries(t, root, "A", "tvdb:1"), mustSeriesPath(t, "A")); err != nil {
		t.Fatalf("Put A: %v", err)
	}

	err := index.Put(testIndexSeries(t, root, "B", "tvdb:1"), mustSeriesPath(t, "B"))
	var duplicate DuplicateLibraryIndexRefError
	if !errors.As(err, &duplicate) {
		t.Fatalf("Put B error = %v, want DuplicateLibraryIndexRefError", err)
	}
	if duplicate.Existing.String() != "A" || duplicate.Next.String() != "B" {
		t.Fatalf("duplicate = %+v, want existing A next B", duplicate)
	}
}

func testIndexRoot(t *testing.T) fsroot.LibraryRoot {
	t.Helper()
	root, err := fsroot.ParseLibraryRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	return root
}

func saveIndexSeries(t *testing.T, root fsroot.LibraryRoot, name string, ref string) Series {
	t.Helper()
	dir := root.Join(name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir %s: %v", name, err)
	}
	series := testIndexSeries(t, root, name, ref)
	if err := SaveSeries(series); err != nil {
		t.Fatalf("SaveSeries %s: %v", name, err)
	}
	return series
}

func testIndexSeries(t *testing.T, root fsroot.LibraryRoot, name string, ref string) Series {
	t.Helper()
	series, err := NewSeries(root.Join(name))
	if err != nil {
		t.Fatalf("NewSeries %s: %v", name, err)
	}
	series.MetadataRef = ref
	series.PreferredTitle = name
	series.CanonicalTitle = name
	return *series
}

func mustSeriesPath(t *testing.T, name string) domain.SeriesPath {
	t.Helper()
	path, err := domain.ParseSeriesPath(name)
	if err != nil {
		t.Fatalf("ParseSeriesPath %s: %v", name, err)
	}
	return path
}
