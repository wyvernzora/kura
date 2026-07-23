package mediainfo

import "testing"

func TestInferResolutionFromFilename(t *testing.T) {
	cases := map[string]string{
		// Explicit dimensions in suffix.
		"Foo - S01E01 (1920x1080).mkv":       "1920x1080",
		"Foo - S01E01 (BluRay 1280x720).mkv": "1280x720",
		"Foo - S01E01 (3840x2160 HDR).mkv":   "3840x2160",

		// Shorthand tags map to canonical dimensions.
		"Foo - S01E01 (BluRay 1080p).mkv": "1920x1080",
		"Foo - S01E01 (WebRip 720p).mkv":  "1280x720",
		"Foo - S01E01 (480p).mkv":         "854x480",
		"Foo - S01E01 (4K BluRay).mkv":    "3840x2160",
		"Foo - S01E01 (2160p Web-DL).mkv": "3840x2160",
		"Foo - S01E01 (1440p).mkv":        "2560x1440",

		// Suffix with no resolution token.
		"Foo - S01E01 (BluRay).mkv":     "",
		"Foo - S01E01 (x265 10bit).mkv": "",
		"Foo - S01E01.mkv":              "",
	}
	for in, want := range cases {
		if got := InferResolutionFromFilename(in); got != want {
			t.Errorf("InferResolutionFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
