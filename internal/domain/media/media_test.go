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
	if ParseSource("bdrip").Rank() <= source.Rank() {
		t.Fatal("BDRip rank should be above Web-DL rank")
	}
}

func TestParseSourceAliases(t *testing.T) {
	cases := map[string]Source{
		"BD":     SourceBDRip,
		"bd":     SourceBDRip,
		"BD-Rip": SourceBDRip,
		"BDMV":   SourceBluRay,
		"BDISO":  SourceBluRay,
	}
	for in, want := range cases {
		if got := ParseSource(in); got != want {
			t.Fatalf("ParseSource(%q) = %q, want %q", in, got, want)
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
