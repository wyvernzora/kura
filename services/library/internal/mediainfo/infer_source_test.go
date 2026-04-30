package mediainfo

import "testing"

func TestInferSourceFromFilename(t *testing.T) {
	cases := map[string]string{
		// Canonical kura filenames: source token first.
		"Foo - S01E01 (BluRay 1080p).mkv": "BluRay",
		"Foo - S01E01 (WebRip 720p).mp4":  "WebRip",
		"Foo - S01E01 (BD 1080p).mkv":     "BD",
		"Foo - S01E01 (Web-DL 1080p).mkv": "Web-DL",

		// Source token deeper in the suffix; pick the first known one.
		"Foo - S01E01 (1080p WebRip).mkv":     "WebRip",
		"Foo - S01E01 (1280x720 BD x265).mp4": "BD",

		// Suffix with only a resolution / codec / nothing recognized.
		"Foo - S01E01 (1280x720).mp4":   "unknown",
		"Foo - S01E01 (1080p).mkv":      "unknown",
		"Foo - S01E01 (x265 10bit).mkv": "unknown",
		"Foo - S01E01.mkv":              "unknown",
	}
	for in, want := range cases {
		if got := InferSourceFromFilename(in); got != want {
			t.Errorf("InferSourceFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
