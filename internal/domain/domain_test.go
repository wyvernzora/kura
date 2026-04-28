package domain

import (
	"testing"
)

func TestFileTitleNormalizesAndCompares(t *testing.T) {
	title, err := ParseFileTitle(" 本好きの下剋上 司書になるためには手段を選んでいられません ")
	if err != nil {
		t.Fatalf("ParseFileTitle: %v", err)
	}
	if !title.EqualName("本好きの下剋上 司書になるためには手段を選んでいられません") {
		t.Fatal("EqualName = false, want NFC-equivalent title match")
	}
	if _, err := ParseFileTitle("Bad/Title"); err == nil {
		t.Fatal("ParseFileTitle returned nil error, want separator rejection")
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
	if resolution.Display() != "1080p" {
		t.Fatalf("Display = %q, want 1080p", resolution.Display())
	}
	displayCases := map[string]string{
		"3840x2160": "4K",
		"2560x1440": "1440p",
		"1280x720":  "720p",
		"854x480":   "480p",
		"1920x800":  "1920x800",
	}
	for input, want := range displayCases {
		resolution, err := ParseResolution(input)
		if err != nil {
			t.Fatalf("ParseResolution(%q): %v", input, err)
		}
		if got := resolution.Display(); got != want {
			t.Fatalf("Display(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParseResolution("1080p"); err == nil {
		t.Fatal("ParseResolution returned nil error, want malformed value rejection")
	}
}

func TestBuildMediaFilename(t *testing.T) {
	title, err := ParseFileTitle("Bookworm")
	if err != nil {
		t.Fatalf("ParseFileTitle: %v", err)
	}
	season, _ := RegularSeason(1)
	episode, _ := NewEpisodeNumber(1)
	resolution, _ := ParseResolution("1920x1080")
	filename := BuildMediaFilename(title, NewEpisodeRef(season, episode), MediaFilenameFacts{
		Source:     ParseMediaSource("webrip"),
		Resolution: resolution,
	}, ".mkv")
	if filename.String() != "Bookworm - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("filename = %q", filename.String())
	}
}
