package seriesfile_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

var update = flag.Bool("update", false, "regenerate golden test fixtures")

const fixtureName = "sample_series.json"

func TestLoadDecodesFixture(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)

	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if model.Ref != ref {
		t.Fatalf("Ref = %s, want %s", model.Ref, ref)
	}
	if string(model.Metadata) != "tvdb:370070" {
		t.Fatalf("Metadata = %s", model.Metadata)
	}
	if model.PreferredTitle.String() != "Bookworm" {
		t.Fatalf("PreferredTitle = %q", model.PreferredTitle)
	}
	if got := len(model.Episodes); got != 3 {
		t.Fatalf("len(Episodes) = %d, want 3", got)
	}

	episode1, _ := refs.NewEpisode(1, 1)
	ep := model.Episodes[episode1]
	if ep.Active == nil {
		t.Fatal("S01E01 active record missing")
	}
	wantPath := filepath.Join(libRoot, "Bookworm", "Season 1", "Bookworm - S01E01 (BDRip 1080p).mkv")
	if ep.Active.Path != wantPath {
		t.Fatalf("active path = %q, want %q (absolutized)", ep.Active.Path, wantPath)
	}
	if got := len(ep.Active.Companions); got != 1 {
		t.Fatalf("companion count = %d, want 1", got)
	}
	if !filepath.IsAbs(ep.Active.Companions[0].Path) {
		t.Fatalf("companion path %q not absolute", ep.Active.Companions[0].Path)
	}

	episode2, _ := refs.NewEpisode(1, 2)
	staged := model.Episodes[episode2].Staged
	if staged == nil {
		t.Fatal("S01E02 staged record missing")
	}
	if staged.Path != "/inbox/Bookworm S01E02.mkv" {
		t.Fatalf("staged path = %q (must stay absolute as-is)", staged.Path)
	}
}

func TestSaveRoundTripBytes(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	original, err := os.ReadFile(paths.SeriesMetadata(libRoot, ref))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	model, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := seriesfile.Save(libRoot, model); err != nil {
		t.Fatalf("Save: %v", err)
	}
	saved, err := os.ReadFile(paths.SeriesMetadata(libRoot, ref))
	if err != nil {
		t.Fatalf("ReadFile after Save: %v", err)
	}

	if *update {
		if err := os.WriteFile(filepath.Join("testdata", fixtureName), saved, 0o644); err != nil {
			t.Fatalf("update fixture: %v", err)
		}
		return
	}
	if !bytes.Equal(original, saved) {
		t.Fatalf("round-trip mismatch\n--- want ---\n%s\n--- got ---\n%s", original, saved)
	}
}

func TestSaveErrorsOnZeroRef(t *testing.T) {
	libRoot := t.TempDir()
	model := &series.Series{
		Metadata: refs.Metadata("tvdb:1"),
	}
	err := seriesfile.Save(libRoot, model)
	if err == nil || !strings.Contains(err.Error(), "zero Ref") {
		t.Fatalf("Save error = %v, want zero Ref error", err)
	}
}

func TestSaveErrorsOnNil(t *testing.T) {
	if err := seriesfile.Save(t.TempDir(), nil); err == nil {
		t.Fatal("Save(nil) returned nil error")
	}
}

func TestExistsReportsPresence(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	present, err := seriesfile.Exists(libRoot, ref)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !present {
		t.Fatal("Exists = false, want true")
	}

	missing, _ := refs.ParseSeries("Missing")
	present, err = seriesfile.Exists(libRoot, missing)
	if err != nil {
		t.Fatalf("Exists missing: %v", err)
	}
	if present {
		t.Fatal("Exists missing = true, want false")
	}
}

func TestLoadRejectsUnknownSchemaVersion(t *testing.T) {
	libRoot, ref := setupFixtureLibrary(t)
	bad := []byte(`{"schemaVersion":2,"metadataRef":"tvdb:1","episodes":{}}`)
	if err := os.WriteFile(paths.SeriesMetadata(libRoot, ref), bad, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := seriesfile.Load(libRoot, ref); err == nil || !strings.Contains(err.Error(), "schemaVersion") {
		t.Fatalf("Load error = %v, want schemaVersion error", err)
	}
}

// setupFixtureLibrary builds a temp library with the sample series.json
// installed at <root>/Bookworm/.kura/series.json. Returns root + ref.
func setupFixtureLibrary(t *testing.T) (string, refs.Series) {
	t.Helper()
	libRoot := t.TempDir()
	ref, err := refs.ParseSeries("Bookworm")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	dst := paths.SeriesMetadata(libRoot, ref)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	src, err := os.ReadFile(filepath.Join("testdata", fixtureName))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatalf("install fixture: %v", err)
	}
	return libRoot, ref
}
