package series

import "testing"

func TestInferEpisodeFromFilenameUsesRegexStrategyFirst(t *testing.T) {
	season, episode, ok := inferEpisodeFromFilename("Bookworm - S02E03 (WebRip 1080p).mkv")
	if !ok {
		t.Fatal("InferEpisodeFromFilename ok = false, want true")
	}
	if season != 2 || episode != 3 {
		t.Fatalf("ref = S%dE%d, want S2E3", season, episode)
	}
}

func TestInferEpisodeFromFilenameUsesAnitogoFallback(t *testing.T) {
	season, episode, ok := inferEpisodeFromFilename("[SubsPlease] Sousou no Frieren - 12 (1080p) [ABC12345].mkv")
	if !ok {
		t.Fatal("InferEpisodeFromFilename ok = false, want true")
	}
	if season != -1 || episode != 12 {
		t.Fatalf("ref = S%dE%d, want unknown season episode 12", season, episode)
	}
}

func TestInferEpisodeFromFilenameUsesAnitogoSeason(t *testing.T) {
	season, episode, ok := inferEpisodeFromFilename("[Conclave-Mendoi]_Mobile_Suit_Gundam_00_S2_-_01v2_[1280x720_H.264_AAC][4863FBE8].mkv")
	if !ok {
		t.Fatal("InferEpisodeFromFilename ok = false, want true")
	}
	if season != 2 || episode != 1 {
		t.Fatalf("ref = S%dE%d, want S2E1", season, episode)
	}
}

func TestInferEpisodeFromFilenameRejectsAmbiguousAnitogoEpisodeRange(t *testing.T) {
	if season, episode, ok := inferEpisodeFromFilename("[Tsundere] Hyouka - 01v2-04 [BDRip h264 1920x1080 10bit FLAC].mkv"); ok {
		t.Fatalf("InferEpisodeFromFilename = S%dE%d ok, want ambiguous range rejected", season, episode)
	}
}

func TestInferEpisodeFromFilenameRejectsZeroEpisodeFallback(t *testing.T) {
	if season, episode, ok := inferEpisodeFromFilename("[gg]_Kimi_ni_Todoke_2nd_Season_-_00_[BF735BC4].mkv"); ok {
		t.Fatalf("InferEpisodeFromFilename = S%dE%d ok, want zero episode rejected", season, episode)
	}
}
