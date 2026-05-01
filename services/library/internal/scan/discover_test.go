package scan

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wyvernzora/kura/internal/storage/seriesdir"
)

func TestDiscoverSeasonEpisodesUsesAnitogoFallback(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Frieren")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeScanTestFile(t, filepath.Join(seasonDir, "[SubsPlease] Sousou no Frieren - 12 (1080p) [ABC12345].mkv"))

	dir, err := seriesdir.Parse(seriesDir)
	if err != nil {
		t.Fatalf("seriesdir.Parse: %v", err)
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
	if episodes[0].Ref.Season() != 1 || episodes[0].Ref.Episode() != 12 {
		t.Fatalf("episode = %s, want S1E12", episodes[0].Ref)
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

	dir, err := seriesdir.Parse(seriesDir)
	if err != nil {
		t.Fatalf("seriesdir.Parse: %v", err)
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

	dir, err := seriesdir.Parse(seriesDir)
	if err != nil {
		t.Fatalf("seriesdir.Parse: %v", err)
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
	if err := os.MkdirAll(filepath.Join(seriesDir, "Downloads"), 0o755); err != nil {
		t.Fatalf("MkdirAll Downloads: %v", err)
	}

	dir, err := seriesdir.Parse(seriesDir)
	if err != nil {
		t.Fatalf("seriesdir.Parse: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(episodes) != 0 {
		t.Fatalf("episodes = %#v, want none", episodes)
	}
	want := map[string]string{
		"Downloads": SkipCodeIgnoredDirectory,
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

func TestDiscoverSeriesEpisodesIgnoresSeasonExtraDirectory(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Frieren")
	extraDir := filepath.Join(seriesDir, "Season 1", "Extra")
	if err := os.MkdirAll(extraDir, 0o755); err != nil {
		t.Fatalf("MkdirAll Extra: %v", err)
	}
	writeScanTestFile(t, filepath.Join(extraDir, "interview.mkv"))

	dir, err := seriesdir.Parse(seriesDir)
	if err != nil {
		t.Fatalf("seriesdir.Parse: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(episodes) != 0 {
		t.Fatalf("episodes = %#v, want none", episodes)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
}

func TestDiscoverSeriesEpisodesFindsCompanions(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeScanTestFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).mkv"))
	writeScanTestFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).en.ass"))
	writeScanTestFile(t, filepath.Join(seasonDir, "Bookworm - S01E01 (WebRip 1080p).nfo"))
	writeScanTestFile(t, filepath.Join(seasonDir, "Bookworm - S01E02 (WebRip 1080p).mkv"))

	dir, err := seriesdir.Parse(seriesDir)
	if err != nil {
		t.Fatalf("seriesdir.Parse: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %#v, want none", skipped)
	}
	if len(episodes) != 2 {
		t.Fatalf("len(episodes) = %d, want 2", len(episodes))
	}
	want := []string{
		"Season 1/Bookworm - S01E01 (WebRip 1080p).en.ass",
		"Season 1/Bookworm - S01E01 (WebRip 1080p).nfo",
	}
	if !slices.Equal(episodes[0].Companions, want) {
		t.Fatalf("companions = %#v, want %#v", episodes[0].Companions, want)
	}
}

func writeScanTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("media"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func TestDiscoverSeasonEpisodesRejectsDuplicateSlots(t *testing.T) {
	root := t.TempDir()
	seriesDir := filepath.Join(root, "Frieren")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeScanTestFile(t, filepath.Join(seasonDir, "Frieren - S01E01 (WebRip 1080p).mkv"))
	writeScanTestFile(t, filepath.Join(seasonDir, "[SubsPlease] Frieren - 01 (1080p).mkv"))

	dir, err := seriesdir.Parse(seriesDir)
	if err != nil {
		t.Fatalf("seriesdir.Parse: %v", err)
	}
	episodes, skipped, err := DiscoverSeriesEpisodes(dir)
	if err != nil {
		t.Fatalf("DiscoverSeriesEpisodes: %v", err)
	}
	if len(episodes) != 0 {
		t.Fatalf("episodes = %v, want both files dropped", episodes)
	}
	dups := 0
	for _, skip := range skipped {
		if skip.Code == SkipCodeDuplicateSlot {
			dups++
		}
	}
	if dups != 2 {
		t.Fatalf("duplicate-slot skips = %d, want 2; skipped = %v", dups, skipped)
	}
}
