package kura

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/store"
)

func TestReadOverlaysLocalMediaOntoMetadataEpisodes(t *testing.T) {
	rootPath := t.TempDir()
	seriesDir := filepath.Join(rootPath, "Bookworm")
	seasonDir := filepath.Join(seriesDir, "Season 1")
	if err := os.MkdirAll(seasonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll season: %v", err)
	}
	writeFile(t, filepath.Join(seasonDir, "episode-1.mkv"), "episode 1")
	writeFile(t, filepath.Join(seasonDir, "episode-6.mkv"), "episode 6")
	stagedDir := t.TempDir()
	stagedFive := filepath.Join(stagedDir, "episode-5.mkv")
	stagedSix := filepath.Join(stagedDir, "episode-6-bd.mkv")
	writeFile(t, stagedFive, "episode 5")
	writeFile(t, stagedSix, "episode 6 bd")

	writeSeriesJSON(t, seriesDir, `{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"preferredTitle": "Bookworm",
		"canonicalTitle": "Ascendance of a Bookworm",
		"seasons": [
			{
				"number": 1,
				"episodes": [
					{
						"number": 1,
						"media": {
							"path": "Season 1/episode-1.mkv",
							"source": "webrip",
							"size": 9,
							"mtime": "2026-04-20T03:00:00Z",
							"mediainfo": {"resolution": "1920x1080"}
						},
						"companions": []
					},
					{
						"number": 4,
						"media": {
							"path": "Season 1/missing-file.mkv",
							"source": "web-dl",
							"size": 9,
							"mtime": "2026-04-20T03:00:00Z",
							"mediainfo": {"resolution": "1280x720"}
						},
						"companions": []
					},
					{
						"number": 6,
						"media": {
							"path": "Season 1/episode-6.mkv",
							"source": "webrip",
							"size": 9,
							"mtime": "2026-04-20T03:00:00Z",
							"mediainfo": {"resolution": "1920x1080"}
						},
						"companions": []
					}
				]
			}
		]
	}`)
	if err := os.WriteFile(filepath.Join(seriesDir, ".kura", "staged.json"), []byte(fmt.Sprintf(`{
		"schemaVersion": 1,
		"entries": [
			{
				"season": 1,
				"number": 5,
				"media": {
					"path": %q,
					"source": "bluray",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"mediainfo": {"resolution": "3840x2160"}
				},
				"companions": []
			},
			{
				"season": 1,
				"number": 6,
				"media": {
					"path": %q,
					"source": "bluray",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"mediainfo": {"resolution": "3840x2160"}
				},
				"companions": []
			}
		]
	}`, stagedFive, stagedSix)), 0o644); err != nil {
		t.Fatalf("WriteFile staged.json: %v", err)
	}

	lib := newReadTestLibrary(t, rootPath, readFakeSource{series: readTestMetadataSeries()})
	series, err := lib.Get("Bookworm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	view, err := series.Read(context.Background(), ReadInput{
		Now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	episodes := view.Seasons[0].Episodes
	if len(episodes) != 6 {
		t.Fatalf("len(Episodes) = %d, want 6", len(episodes))
	}
	wantStatuses := []EpisodeStatus{
		EpisodeStatusPresent,
		EpisodeStatusMissing,
		EpisodeStatusPending,
		EpisodeStatusUnavailable,
		EpisodeStatusStaged,
		EpisodeStatusStaged,
	}
	for index, want := range wantStatuses {
		if episodes[index].Status != want {
			t.Fatalf("episode %d status = %q, want %q", index+1, episodes[index].Status, want)
		}
	}
	if episodes[0].Active == nil || episodes[0].Active.Source != "WebRip" || episodes[0].Active.Resolution != "1080p" {
		t.Fatalf("episode 1 active media = %#v, want WebRip 1080p", episodes[0].Active)
	}
	if episodes[4].Staged == nil || episodes[4].Staged.Source != "BluRay" || episodes[4].Staged.Resolution != "4K" {
		t.Fatalf("episode 5 staged media = %#v, want BluRay 4K", episodes[4].Staged)
	}
	if episodes[5].Active == nil || episodes[5].Staged == nil {
		t.Fatalf("episode 6 active/staged = %#v/%#v, want both", episodes[5].Active, episodes[5].Staged)
	}
}

func newReadTestLibrary(t *testing.T, rootPath string, source metadata.Source) *Library {
	t.Helper()
	root, err := fsroot.ParseLibraryRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseLibraryRoot: %v", err)
	}
	index, err := store.RebuildLibraryIndex(context.Background(), root)
	if err != nil {
		t.Fatalf("RebuildLibraryIndex: %v", err)
	}
	return &Library{
		root:           root,
		metadataSource: source,
		index:          index,
	}
}

type readFakeSource struct {
	series metadata.Series
}

func (s readFakeSource) Key() string {
	return "tvdb"
}

func (s readFakeSource) Search(context.Context, string, metadata.SearchOptions) ([]metadata.SearchResult, error) {
	return nil, nil
}

func (s readFakeSource) GetSeries(context.Context, string) (metadata.Series, error) {
	return s.series, nil
}

func readTestMetadataSeries() metadata.Series {
	return metadata.Series{
		SeriesSummary: metadata.SeriesSummary{
			MetadataRef:    "tvdb:370070",
			PreferredTitle: "Bookworm",
			CanonicalTitle: "Ascendance of a Bookworm",
		},
		Seasons: []metadata.Season{
			{
				MetadataRef: "tvdb:10",
				Number:      1,
				Episodes: []metadata.Episode{
					readTestMetadataEpisode(1, "2019-10-03"),
					readTestMetadataEpisode(2, "2019-10-10"),
					readTestMetadataEpisode(3, "2026-04-30"),
					readTestMetadataEpisode(4, "2019-10-24"),
					readTestMetadataEpisode(5, "2019-10-31"),
					readTestMetadataEpisode(6, "2019-11-07"),
				},
			},
		},
	}
}

func readTestMetadataEpisode(number int, aired string) metadata.Episode {
	return metadata.Episode{
		MetadataRef:   fmt.Sprintf("tvdb:%d", 1000+number),
		SeasonNumber:  1,
		EpisodeNumber: number,
		Aired:         aired,
	}
}
