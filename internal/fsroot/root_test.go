package fsroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLibraryRootAndSeriesDir(t *testing.T) {
	rootPath := t.TempDir()
	seriesPath := filepath.Join(rootPath, "Bookworm")
	if err := os.Mkdir(seriesPath, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	root, err := ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	seriesDir, err := root.SeriesDir("Bookworm")
	if err != nil {
		t.Fatalf("SeriesDir: %v", err)
	}
	if seriesDir.Path() != seriesPath {
		t.Fatalf("Path = %q, want %q", seriesDir.Path(), seriesPath)
	}
	if seriesDir.MetadataPath() != filepath.Join(seriesPath, ".kura", "series.json") {
		t.Fatalf("MetadataPath = %q", seriesDir.MetadataPath())
	}
	if !root.Contains(filepath.Join(seriesPath, "episode.mkv")) {
		t.Fatal("root does not contain child path")
	}
	if root.Contains(filepath.Dir(rootPath)) {
		t.Fatal("root contains parent path")
	}
}

func TestSeriesDirRejectsEscapingRelPath(t *testing.T) {
	seriesDir, err := ParseSeriesDir(t.TempDir())
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	if _, err := seriesDir.CleanRelPath("../episode.mkv"); err == nil {
		t.Fatal("CleanRelPath returned nil error, want escaping path rejection")
	}
	if _, err := seriesDir.CleanRelPath(".kura/series.json"); err == nil {
		t.Fatal("CleanRelPath returned nil error, want .kura path rejection")
	}
}
