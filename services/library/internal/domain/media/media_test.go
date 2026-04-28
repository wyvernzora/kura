package media

import "testing"

func TestSourceDisplayAndRank(t *testing.T) {
	source := ParseSource("webdl")
	if source.String() != "web-dl" {
		t.Fatalf("String = %q, want web-dl", source.String())
	}
	if source.Display() != "Web-DL" {
		t.Fatalf("Display = %q, want Web-DL", source.Display())
	}
	if ParseSource("bluray").Rank() <= source.Rank() {
		t.Fatal("BluRay rank should be above Web-DL rank")
	}
}

func TestParseSourceCollapsesBluRayFamily(t *testing.T) {
	for _, in := range []string{"BD", "bd", "BD-Rip", "BDRip", "bdrip", "BluRay", "Blu-Ray", "BDMV", "BDISO"} {
		got := ParseSource(in)
		if got != SourceBluRay {
			t.Fatalf("ParseSource(%q) = %q, want %q", in, got, SourceBluRay)
		}
		if got.Display() != "BluRay" {
			t.Fatalf("ParseSource(%q).Display() = %q, want BluRay", in, got.Display())
		}
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
		// Standard tiers.
		"3840x2160": "4K",
		"2560x1440": "1440p",
		"1280x720":  "720p",
		"854x480":   "480p",
		// 4:3 variants fold by height — operator can't acquire 16:9.
		"1440x1080": "1080p",
		"960x720":   "720p",
		// Cinemascope / ultrawide bucket by reduced height.
		"1920x800":  "720p",
		"2560x1080": "1080p",
		// Near-standard ±5% encodes fold via tolerant boundaries.
		"1328x720":  "720p",
		"1272x712":  "720p",
		"1916x1076": "1080p",
		// Below the 360p floor falls through to raw WxH.
		"320x240": "320x240",
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
