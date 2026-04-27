package fsroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSeasonEpisodesUsesAnitogoFallback(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Frieren")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeScanTestFile(t, filepath.Join(seasonDir, "[SubsPlease] Sousou no Frieren - 12 (1080p) [ABC12345].mkv"))

	dir, err := ParseSeriesDir(seriesDir)
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
	if len(episodes) != 1 {
		t.Fatalf("len(episodes) = %d, want 1", len(episodes))
	}
	if episodes[0].Season != 1 || episodes[0].Number != 12 {
		t.Fatalf("episode = S%dE%d, want S1E12", episodes[0].Season, episodes[0].Number)
	}
}

func TestDiscoverSeasonEpisodesRejectsFallbackSeasonMismatch(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Gundam")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeScanTestFile(t, filepath.Join(seasonDir, "[Conclave-Mendoi]_Mobile_Suit_Gundam_00_S2_-_01v2_[1280x720_H.264_AAC][4863FBE8].mkv"))

	dir, err := ParseSeriesDir(seriesDir)
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(episodes) != 0 {
		t.Fatalf("episodes = %#v, want none", episodes)
	}
	if len(skipped) != 1 || skipped[0].Code != SkipCodeSeasonMismatch {
		t.Fatalf("skipped = %#v, want season mismatch", skipped)
	}
}

func TestDiscoverSeriesRootRejectsImplicitFallbackSeason(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Frieren")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeScanTestFile(t, filepath.Join(seriesDir, "[SubsPlease] Sousou no Frieren - 12 (1080p) [ABC12345].mkv"))

	dir, err := ParseSeriesDir(seriesDir)
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(episodes) != 0 {
		t.Fatalf("episodes = %#v, want none", episodes)
	}
	if len(skipped) != 1 || skipped[0].Code != SkipCodeSpecialNumberNotInferred {
		t.Fatalf("skipped = %#v, want special number not inferred", skipped)
	}
}

func TestDiscoverSeriesEpisodesReportsIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Frieren")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(filepath.Join(seriesDir, "Downloads"), 0o755); err != nil {
		t.Fatalf("MkdirAll Downloads: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(seasonDir, "Extra"), 0o755); err != nil {
		t.Fatalf("MkdirAll Extra: %v", err)
	}

	dir, err := ParseSeriesDir(seriesDir)
	if err != nil {
		t.Fatalf("ParseSeriesDir: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(episodes) != 0 {
		t.Fatalf("episodes = %#v, want none", episodes)
	}
	want := map[string]string{
		"Downloads":      SkipCodeIgnoredDirectory,
		"Season 1/Extra": SkipCodeIgnoredDirectory,
	}
	if len(skipped) != len(want) {
		t.Fatalf("skipped = %#v, want ignored directories", skipped)
	}
	for _, skip := range skipped {
		if want[skip.Path] != skip.Code {
			t.Fatalf("skip = %#v, want ignored directory", skip)
		}
	}
}

func writeScanTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
