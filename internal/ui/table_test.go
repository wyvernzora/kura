package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/internal/refs"
	"github.com/wyvernzora/kura/internal/series"
)

func TestShowTableRendersStagedOverAsSeparateRows(t *testing.T) {
	var out bytes.Buffer
	episode, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = writeEpisodeReadTable(&out, []series.Episode{
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

func TestScanTablePrintsTTYEmptyMessage(t *testing.T) {
	var out bytes.Buffer
	if err := writeScanTable(&out, nil, nil, true); err != nil {
		t.Fatalf("writeScanTable: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "No files found.") {
		t.Fatalf("rendered table = %q, want empty message", rendered)
	}
	if strings.Contains(rendered, "STATUS") {
		t.Fatalf("rendered table = %q, want no table header", rendered)
	}
}

func TestScanTableKeepsNonTTYEmptyTable(t *testing.T) {
	var out bytes.Buffer
	if err := writeScanTable(&out, nil, nil, false); err != nil {
		t.Fatalf("writeScanTable: %v", err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "No files found.") {
		t.Fatalf("rendered table = %q, want no empty message", rendered)
	}
	if !strings.Contains(rendered, "STATUS") {
		t.Fatalf("rendered table = %q, want table header", rendered)
	}
}

func TestScanTableRendersEpisodeMarkerAndMediaFacts(t *testing.T) {
	var out bytes.Buffer
	episode, err := refs.NewEpisode(2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeScanTable(&out, []scanTableEntry{{
		Status:     string(series.ScanStatusNew),
		Episode:    episode,
		Source:     "webrip",
		Resolution: "1920x1080",
		Path:       "Season 2/episode.mkv",
	}}, nil, false); err != nil {
		t.Fatalf("writeScanTable: %v", err)
	}
	rendered := out.String()
	for _, want := range []string{"EPISODE", "STATUS", "SOURCE", "RESOLUTION", "FILE", "S02E03", "new", "WebRip", "1080p", "Season 2/episode.mkv"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered table = %q, want %q", rendered, want)
		}
	}
	for _, unwanted := range []string{"SEASON", "1920x1080", "webrip"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("rendered table = %q, want no %q", rendered, unwanted)
		}
	}
}

func TestScanTableStylesTTYMediaFacts(t *testing.T) {
	var out bytes.Buffer
	episode, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeScanTable(&out, []scanTableEntry{{
		Status:     string(series.ScanStatusNew),
		Episode:    episode,
		Source:     "webrip",
		Resolution: "1920x1080",
		Path:       "Season 1/episode.mkv",
	}}, nil, true); err != nil {
		t.Fatalf("writeScanTable: %v", err)
	}
	rendered := out.String()
	for _, want := range []string{
		"\x1b[32mnew\x1b[39m",
		"\x1b[33mWebRip\x1b[39m",
		"\x1b[32m1080p\x1b[39m",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered table = %q, want %q", rendered, want)
		}
	}
}

func TestReconcileTablePrintsTTYEmptyMessage(t *testing.T) {
	var out bytes.Buffer
	if err := writeReconcileMoves(&out, nil, true); err != nil {
		t.Fatalf("writeReconcileMoves: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "Nothing to reconcile.") {
		t.Fatalf("rendered table = %q, want empty message", rendered)
	}
	if strings.Contains(rendered, "KIND") {
		t.Fatalf("rendered table = %q, want no table header", rendered)
	}
}

func TestReconcileTableKeepsNonTTYEmptyTable(t *testing.T) {
	var out bytes.Buffer
	if err := writeReconcileMoves(&out, nil, false); err != nil {
		t.Fatalf("writeReconcileMoves: %v", err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "Nothing to reconcile.") {
		t.Fatalf("rendered table = %q, want no empty message", rendered)
	}
	if !strings.Contains(rendered, "KIND") {
		t.Fatalf("rendered table = %q, want table header", rendered)
	}
}

func TestRenderStatus(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   string
	}{
		{"missing", string(series.EpisodeStatusMissing), "\x1b[38;5;208mmissing\x1b[39m"},
		{"unavailable", string(series.EpisodeStatusUnavailable), "\x1b[1m\x1b[31munavailable\x1b[39m\x1b[22m"},
		{"present", string(series.EpisodeStatusPresent), "\x1b[32mpresent\x1b[39m"},
		{"pending", string(series.EpisodeStatusPending), "\x1b[2m\x1b[90mpending\x1b[39m\x1b[22m"},
		{"staged", string(series.EpisodeStatusStaged), "\x1b[33mstaged\x1b[39m"},
		{"staged replacement", string(series.EpisodeStatusStagedReplacement), "\x1b[33mstaged_replacement\x1b[39m"},
		{"new", string(series.ScanStatusNew), "\x1b[32mnew\x1b[39m"},
		{"existing", string(series.ScanStatusExisting), "\x1b[2m\x1b[90mexisting\x1b[39m\x1b[22m"},
		{"updated", string(series.ScanStatusUpdated), "\x1b[33mupdated\x1b[39m"},
		{"replaced", string(series.ScanStatusReplaced), "\x1b[33mreplaced\x1b[39m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderStatus(tc.status, true); got != tc.want {
				t.Fatalf("renderStatus = %q, want %q", got, tc.want)
			}
			if got := renderStatus(tc.status, false); got != tc.status {
				t.Fatalf("unstyled status = %q, want %q", got, tc.status)
			}
		})
	}
}

func TestRenderMediaSource(t *testing.T) {
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
			if got := renderMediaSource(tc.source, true); !strings.HasPrefix(got, tc.ansi) {
				t.Fatalf("renderMediaSource = %q, want prefix %q", got, tc.ansi)
			}
			if got := renderMediaSource(tc.source, false); got != tc.source {
				t.Fatalf("unstyled source = %q, want %q", got, tc.source)
			}
		})
	}
}

func TestRenderMediaResolution(t *testing.T) {
	cases := []struct {
		resolution string
		plain      string
		ansi       string
	}{
		{"4K", "4K", "\x1b[34m"},
		{"1920x1080", "1080p", "\x1b[32m"},
		{"1280x720", "720p", "\x1b[31m"},
		{"640x480", "480p", "\x1b[38;5;208m"},
	}
	for _, tc := range cases {
		t.Run(tc.resolution, func(t *testing.T) {
			if got := renderMediaResolution(tc.resolution, true); !strings.HasPrefix(got, tc.ansi) {
				t.Fatalf("renderMediaResolution = %q, want prefix %q", got, tc.ansi)
			}
			if got := renderMediaResolution(tc.resolution, false); got != tc.plain {
				t.Fatalf("unstyled resolution = %q, want %q", got, tc.plain)
			}
		})
	}
}
