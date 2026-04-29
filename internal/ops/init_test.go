package ops

import (
	"os"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

func TestInitSeriesPopulatesFromMetadata(t *testing.T) {
	seriesDir := testSeriesDir(t, "Bookworm")
	metadataSeries := testMetadataSeries()

	result, err := InitSeries(InitSeriesOptions{SeriesDir: seriesDir, Metadata: metadataSeries})
	if err != nil {
		t.Fatalf("InitSeries: %v", err)
	}
	if result.Series.MetadataRef != metadataSeries.MetadataRef {
		t.Fatalf("MetadataRef = %q, want %q", result.Series.MetadataRef, metadataSeries.MetadataRef)
	}
	if result.Series.PreferredTitle != metadataSeries.PreferredTitle {
		t.Fatalf("PreferredTitle = %q, want %q", result.Series.PreferredTitle, metadataSeries.PreferredTitle)
	}
	if result.Series.CanonicalTitle != metadataSeries.CanonicalTitle {
		t.Fatalf("CanonicalTitle = %q, want %q", result.Series.CanonicalTitle, metadataSeries.CanonicalTitle)
	}
	if result.Series.SchemaVersion != store.SeriesSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", result.Series.SchemaVersion, store.SeriesSchemaVersion)
	}
	if result.SeriesPath.String() != "Bookworm" {
		t.Fatalf("SeriesPath = %q, want Bookworm", result.SeriesPath.String())
	}
}

func TestInitSeriesRejectsUnsupportedMetadataSource(t *testing.T) {
	seriesDir := testSeriesDir(t, "Bookworm")
	metadataSeries := testMetadataSeries()
	metadataSeries.MetadataRef = "imdb:tt123"

	_, err := InitSeries(InitSeriesOptions{SeriesDir: seriesDir, Metadata: metadataSeries})
	if err == nil {
		t.Fatal("InitSeries returned nil error, want unsupported source error")
	}
	if !strings.Contains(err.Error(), "unsupported metadata ref source") {
		t.Fatalf("error = %v, want unsupported metadata ref source", err)
	}
}

func TestInitSeriesRejectsEmptyMetadataRef(t *testing.T) {
	seriesDir := testSeriesDir(t, "Bookworm")
	metadataSeries := metadata.Series{}

	_, err := InitSeries(InitSeriesOptions{SeriesDir: seriesDir, Metadata: metadataSeries})
	if err == nil {
		t.Fatal("InitSeries returned nil error, want metadata ref error")
	}
}

func testSeriesDir(t *testing.T, name string) fsroot.SeriesDir {
	t.Helper()
	root, err := fsroot.ParseLibraryRoot(t.TempDir())
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	path := root.Join(name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seriesDir, err := fsroot.ParseSeriesDir(path)
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	return seriesDir
}
