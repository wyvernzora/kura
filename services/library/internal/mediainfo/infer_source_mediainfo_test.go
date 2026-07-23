package mediainfo

import (
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/domain/media"
)

func TestInferSourceFromMediainfo(t *testing.T) {
	cases := []struct {
		name string
		info media.Info
		path string
		want string
	}{
		// Title beats container ext beats audio codec.
		{
			name: "title carries explicit source",
			info: media.Info{Title: "Foo S01E01 BluRay 1080p", AudioCodec: "AAC"},
			path: "Season 1/foo.mkv",
			want: "BluRay",
		},
		// Container extension as fallback.
		{
			name: "m2ts container implies BluRay",
			info: media.Info{},
			path: "Season 1/00001.m2ts",
			want: "bluray",
		},
		{
			name: "webm container implies WebRip",
			info: media.Info{},
			path: "Season 1/foo.webm",
			want: "webrip",
		},
		// Audio codec as last-resort heuristic.
		{
			name: "TrueHD audio implies BluRay",
			info: media.Info{AudioCodec: "MLP FBA 16-ch"},
			path: "Season 1/foo.mkv",
			want: "bluray",
		},
		{
			name: "DTS-HD MA audio implies BluRay",
			info: media.Info{AudioCodec: "DTS XLL"},
			path: "Season 1/foo.mkv",
			want: "bluray",
		},
		// Generic container + ambiguous audio = no signal.
		{
			name: "mkv with AAC stays empty",
			info: media.Info{AudioCodec: "AAC"},
			path: "Season 1/foo.mkv",
			want: "",
		},
		{
			name: "ts container is ambiguous (broadcast vs raw BD)",
			info: media.Info{},
			path: "Season 1/foo.ts",
			want: "",
		},
		// Empty info with empty path — nothing.
		{
			name: "empty info empty path",
			info: media.Info{},
			path: "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InferSourceFromMediainfo(tc.info, tc.path); got != tc.want {
				t.Errorf("InferSourceFromMediainfo = %q, want %q", got, tc.want)
			}
		})
	}
}
