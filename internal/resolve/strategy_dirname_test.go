package resolve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

func TestDirnameStrategyMissingDir(t *testing.T) {
	strategy := newTestDirnameStrategy(t, t.TempDir(), &strategyFakeSource{})
	hits, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "missing"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestDirnameStrategyInvalidDirPropagatesError(t *testing.T) {
	strategy := newTestDirnameStrategy(t, t.TempDir(), &strategyFakeSource{})
	_, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "../Bookworm"})
	if err == nil {
		t.Fatal("Resolve error = nil, want invalid directory error")
	}
}

func TestDirnameStrategyMissingSeriesFile(t *testing.T) {
	rootDir := t.TempDir()
	mkdir(t, filepath.Join(rootDir, "Bookworm"))
	strategy := newTestDirnameStrategy(t, rootDir, &strategyFakeSource{})

	hits, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "Bookworm"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("len(hits) = %d, want 0", len(hits))
	}
}

func TestDirnameStrategyCorruptSeriesFile(t *testing.T) {
	rootDir := t.TempDir()
	seriesDir := filepath.Join(rootDir, "Bookworm")
	mkdir(t, filepath.Join(seriesDir, ".kura"))
	writeFile(t, store.SeriesPath(seriesDir), []byte(`{"schemaVersion":`))
	strategy := newTestDirnameStrategy(t, rootDir, &strategyFakeSource{})

	_, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "Bookworm"})
	if !errors.Is(err, ErrCorruptSeriesFile) {
		t.Fatalf("error = %v, want ErrCorruptSeriesFile", err)
	}
}

func TestDirnameStrategyStaleProviderRef(t *testing.T) {
	rootDir := t.TempDir()
	writeTrackedSeries(t, filepath.Join(rootDir, "Bookworm"), []string{"tvdb:1"}, "tvdb")
	strategy := newTestDirnameStrategy(t, rootDir, &strategyFakeSource{seriesErr: metadata.ErrNotFound})

	_, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "Bookworm"})
	if !errors.Is(err, ErrStaleProviderRef) {
		t.Fatalf("error = %v, want ErrStaleProviderRef", err)
	}
}

func TestDirnameStrategyResolveSeries(t *testing.T) {
	rootDir := t.TempDir()
	writeTrackedSeries(t, filepath.Join(rootDir, "Bookworm"), []string{"imdb:tt1", "tvdb:1"}, "tvdb")
	strategy := newTestDirnameStrategy(t, rootDir, &strategyFakeSource{
		series: map[string]metadata.Series{"1": testMetadataSeries("tvdb:1")},
	})

	hits, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "Bookworm"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("len(hits) = %d, want 1", len(hits))
	}
	if hits[0].ProviderRef != "tvdb:1" || hits[0].Rank != 0 {
		t.Fatalf("hit = %#v, want tvdb:1 rank 0", hits[0])
	}
}

func TestDirnameStrategyPropagatesTransportError(t *testing.T) {
	rootDir := t.TempDir()
	writeTrackedSeries(t, filepath.Join(rootDir, "Bookworm"), []string{"tvdb:1"}, "tvdb")
	strategy := newTestDirnameStrategy(t, rootDir, &strategyFakeSource{seriesErr: metadata.ErrUnavailable})

	_, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "Bookworm"})
	if !errors.Is(err, metadata.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestDirnameStrategyDifferentProviderIsCorrupt(t *testing.T) {
	rootDir := t.TempDir()
	writeTrackedSeries(t, filepath.Join(rootDir, "Bookworm"), []string{"tmdb:1"}, "tmdb")
	strategy := newTestDirnameStrategy(t, rootDir, &strategyFakeSource{key: "tvdb"})

	_, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "Bookworm"})
	if !errors.Is(err, ErrCorruptSeriesFile) {
		t.Fatalf("error = %v, want ErrCorruptSeriesFile", err)
	}
}

func TestDirnameStrategyNoProviderRefsIsCorrupt(t *testing.T) {
	rootDir := t.TempDir()
	seriesDir := filepath.Join(rootDir, "Bookworm")
	mkdir(t, filepath.Join(seriesDir, ".kura"))
	writeFile(t, store.SeriesPath(seriesDir), []byte(`{"schemaVersion":1,"id":"01JZ7P0Q2V3W4X5Y6Z7A8B9C0D","providerRefs":[],"preferredProvider":"tvdb","preferredTitle":"Bookworm","canonicalTitle":"Ascendance of a Bookworm"}`))
	strategy := newTestDirnameStrategy(t, rootDir, &strategyFakeSource{})

	_, err := strategy.Resolve(context.Background(), Term{Prefix: "dir", Value: "Bookworm"})
	if !errors.Is(err, ErrCorruptSeriesFile) {
		t.Fatalf("error = %v, want ErrCorruptSeriesFile", err)
	}
}

func TestDirnameStrategyProperties(t *testing.T) {
	strategy := newTestDirnameStrategy(t, t.TempDir(), &strategyFakeSource{})
	if !strategy.Match(Term{Prefix: "dir", Value: "Bookworm"}) {
		t.Fatal("Match dir = false, want true")
	}
	if strategy.Match(Term{Value: "Bookworm"}) {
		t.Fatal("Match text = true, want false")
	}
	if !strategy.Authoritative() {
		t.Fatal("Authoritative = false, want true")
	}
}

func newTestDirnameStrategy(t *testing.T, rootDir string, source metadata.Source) ResolveStrategy {
	t.Helper()
	root, err := fsroot.ParseLibraryRoot(rootDir)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	return NewDirnameStrategy(root, source)
}

func writeTrackedSeries(t *testing.T, seriesDir string, providerRefs []string, preferredProvider string) {
	t.Helper()
	mkdir(t, seriesDir)
	series, err := store.NewSeries(seriesDir)
	if err != nil {
		t.Fatalf("NewSeries: %v", err)
	}
	series.ProviderRefs = providerRefs
	series.PreferredProvider = preferredProvider
	series.PreferredTitle = "Bookworm"
	series.CanonicalTitle = "Ascendance of a Bookworm"
	if err := store.SaveSeries(*series); err != nil {
		t.Fatalf("SaveSeries: %v", err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", path, err)
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
