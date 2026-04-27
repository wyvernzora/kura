package library

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

func TestFilesystemTitleNormalizesAndCompares(t *testing.T) {
	title, err := ParseFilesystemTitle(" 本好きの下剋上 司書になるためには手段を選んでいられません ")
	if err != nil {
		t.Fatalf("ParseFilesystemTitle: %v", err)
	}
	if !title.EqualName("本好きの下剋上 司書になるためには手段を選んでいられません") {
		t.Fatal("EqualName = false, want NFC-equivalent title match")
	}
	if _, err := ParseFilesystemTitle("Bad/Title"); err == nil {
		t.Fatal("ParseFilesystemTitle returned nil error, want separator rejection")
	}
}

func TestEpisodeRefMarker(t *testing.T) {
	season, err := RegularSeason(2)
	if err != nil {
		t.Fatalf("RegularSeason: %v", err)
	}
	episode, err := NewEpisodeNumber(3)
	if err != nil {
		t.Fatalf("NewEpisodeNumber: %v", err)
	}
	if got := NewEpisodeRef(season, episode).Marker(); got != "S02E03" {
		t.Fatalf("Marker = %q, want S02E03", got)
	}

	if got := SpecialsSeason().MarkerPart(); got != "S00" {
		t.Fatalf("special marker = %q, want S00", got)
	}
}

func TestMediaSourceDisplayAndRank(t *testing.T) {
	source := ParseMediaSource("webdl")
	if source.String() != "web-dl" {
		t.Fatalf("String = %q, want web-dl", source.String())
	}
	if source.Display() != "Web-DL" {
		t.Fatalf("Display = %q, want Web-DL", source.Display())
	}
	if ParseMediaSource("bdrip").Rank() <= source.Rank() {
		t.Fatal("BDRip rank should be above Web-DL rank")
	}
}

func TestResolution(t *testing.T) {
	resolution, err := ParseResolution("1920x1080")
	if err != nil {
		t.Fatalf("ParseResolution: %v", err)
	}
	if resolution.Width() != 1920 || resolution.Height() != 1080 {
		t.Fatalf("resolution = %dx%d, want 1920x1080", resolution.Width(), resolution.Height())
	}
	if resolution.String() != "1920x1080" {
		t.Fatalf("String = %q, want 1920x1080", resolution.String())
	}
	if _, err := ParseResolution("1080p"); err == nil {
		t.Fatal("ParseResolution returned nil error, want malformed value rejection")
	}
}

func TestBuildMediaFilename(t *testing.T) {
	title, err := ParseFilesystemTitle("Bookworm")
	if err != nil {
		t.Fatalf("ParseFilesystemTitle: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	resolution, _ := ParseResolution("1920x1080")
	filename := BuildMediaFilename(title, NewEpisodeRef(season, episode), MediaFilenameFacts{
		Source:     ParseMediaSource("webrip"),
		VideoCodec: ParseCodec("HEVC"),
		Resolution: resolution,
	}, ".mkv")
	if filename.String() != "Bookworm - S01E01 (WebRip HEVC 1920x1080).mkv" {
		t.Fatalf("filename = %q", filename.String())
	}
}
