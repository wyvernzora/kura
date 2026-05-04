package mediainfo

import "testing"

func TestInferSourceFromText(t *testing.T) {
	cases := map[string]string{
		// Scene-style names with dot separators.
		"Foo.S01E01.BluRay.1080p.x265-Group": "BluRay",
		"Foo.S01E01.WEB-DL.720p":             "WEB-DL",
		"Foo.S01E01.HDTV.x264":               "HDTV",
		// Bracketed group titles common in anime fansub releases.
		"[GroupName] Foo S01E01 [BluRay 1080p HEVC]": "BluRay",
		"[Group] Foo - 01 [WebRip][1080p]":           "WebRip",
		// Free-text with mixed punctuation.
		"Foo Bar (BDRip 1080p)": "BDRip",
		"Foo / Bar / DVDRip":    "DVDRip",
		// Empty / no recognized token / literal "Unknown" all return "".
		"":                      "",
		"Foo S01E01":            "",
		"Foo S01E01 1080p HEVC": "",
		"Unknown":               "",
		"Foo Unknown S01E01":    "",
	}
	for in, want := range cases {
		if got := InferSourceFromText(in); got != want {
			t.Errorf("InferSourceFromText(%q) = %q, want %q", in, got, want)
		}
	}
}
