package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

func TestFindTableRendersStagedOverAsSeparateRows(t *testing.T) {
	var out bytes.Buffer
	episode, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = writeEpisodeReadTable(&out, []series.EpisodeRead{
		{
			Episode: episode,
			Status:  series.EpisodeStatusStaged,
			Active: &series.EpisodeMedia{
				Source:     "WebRip",
				Resolution: "1080p",
				File:       "Season 1/old.mkv",
			},
			Staged: &series.EpisodeMedia{
				Source:     "BDRip",
				Resolution: "4K",
				File:       "/inbox/new.mkv",
			},
		},
	})
	if err != nil {
		t.Fatalf("writeEpisodeReadTable: %v", err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "->") {
		t.Fatalf("rendered table = %q, want no arrow notation", rendered)
	}
	activeIndex := strings.Index(rendered, "present")
	stagedIndex := strings.Index(rendered, "staged")
	if activeIndex < 0 || stagedIndex < 0 || activeIndex > stagedIndex {
		t.Fatalf("rendered table = %q, want present row before staged row", rendered)
	}
	for _, want := range []string{"Season 1/old.mkv", "/inbox/new.mkv"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered table = %q, want %q", rendered, want)
		}
	}
}

func TestFindTableStylesEpisodeStatus(t *testing.T) {
	cases := []struct {
		name   string
		status series.EpisodeStatus
		want   string
	}{
		{"missing", series.EpisodeStatusMissing, "\x1b[38;5;208mmissing\x1b[39m"},
		{"unavailable", series.EpisodeStatusUnavailable, "\x1b[1m\x1b[31munavailable\x1b[39m\x1b[22m"},
		{"present", series.EpisodeStatusPresent, "\x1b[32mpresent\x1b[39m"},
		{"pending", series.EpisodeStatusPending, "\x1b[2m\x1b[90mpending\x1b[39m\x1b[22m"},
		{"staged", series.EpisodeStatusStaged, "\x1b[33mstaged\x1b[39m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := styleEpisodeStatus(tc.status, true); got != tc.want {
				t.Fatalf("styleEpisodeStatus = %q, want %q", got, tc.want)
			}
			if got := styleEpisodeStatus(tc.status, false); got != string(tc.status) {
				t.Fatalf("unstyled status = %q, want %q", got, tc.status)
			}
		})
	}
}

func TestFindTableStylesMediaSource(t *testing.T) {
	cases := []struct {
		source string
		ansi   string
	}{
		{"BDRip", "\x1b[32m"},
		{"BluRay", "\x1b[32m"},
		{"Web-DL", "\x1b[33m"},
		{"WebRip", "\x1b[33m"},
		{"HDTV", "\x1b[38;5;208m"},
		{"Unknown", "\x1b[31m"},
	}
	for _, tc := range cases {
		t.Run(tc.source, func(t *testing.T) {
			if got := styleMediaSource(tc.source, true); !strings.HasPrefix(got, tc.ansi) {
				t.Fatalf("styleMediaSource = %q, want prefix %q", got, tc.ansi)
			}
			if got := styleMediaSource(tc.source, false); got != tc.source {
				t.Fatalf("unstyled source = %q, want %q", got, tc.source)
			}
		})
	}
}

func TestFindTableStylesMediaResolution(t *testing.T) {
	cases := []struct {
		resolution string
		ansi       string
	}{
		{"4K", "\x1b[34m"},
		{"1080p", "\x1b[32m"},
		{"720p", "\x1b[31m"},
		{"480p", "\x1b[38;5;208m"},
	}
	for _, tc := range cases {
		t.Run(tc.resolution, func(t *testing.T) {
			if got := styleMediaResolution(tc.resolution, true); !strings.HasPrefix(got, tc.ansi) {
				t.Fatalf("styleMediaResolution = %q, want prefix %q", got, tc.ansi)
			}
			if got := styleMediaResolution(tc.resolution, false); got != tc.resolution {
				t.Fatalf("unstyled resolution = %q, want %q", got, tc.resolution)
			}
		})
	}
}
