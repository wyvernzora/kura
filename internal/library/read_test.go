package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/mediainfo"
	"github.com/wyvernzora/kura/internal/refs"
	seriespkg "github.com/wyvernzora/kura/internal/series"
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

	writeSeriesJSON(t, seriesDir, fmt.Sprintf(`{
		"schemaVersion": 1,
		"metadataRef": "tvdb:370070",
		"episodes": {
			"S01E0001": {
				"season": 1,
				"episode": 1,
				"airDate": "2019-10-03",
				"active": {
					"path": "Season 1/episode-1.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			},
			"S01E0002": {"season": 1, "episode": 2, "airDate": "2019-10-10"},
			"S01E0003": {"season": 1, "episode": 3, "airDate": "2026-04-30"},
			"S01E0004": {
				"season": 1,
				"episode": 4,
				"airDate": "2019-10-24",
				"active": {
					"path": "Season 1/missing-file.mkv",
					"source": "web-dl",
					"resolution": "1280x720",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			},
			"S01E0005": {
				"season": 1,
				"episode": 5,
				"airDate": "2019-10-31",
				"staged": {
					"path": %q,
					"source": "bluray",
					"resolution": "3840x2160",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			},
			"S01E0006": {
				"season": 1,
				"episode": 6,
				"airDate": "2019-11-07",
				"active": {
					"path": "Season 1/episode-6.mkv",
					"source": "webrip",
					"resolution": "1920x1080",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				},
				"staged": {
					"path": %q,
					"source": "bluray",
					"resolution": "3840x2160",
					"size": 9,
					"mtime": "2026-04-20T03:00:00Z",
					"companions": []
				}
			}
		}
	}`, stagedFive, stagedSix))

	lib := newReadTestLibrary(t, rootPath)
	series, err := lib.Open(mustSeries(t, "Bookworm"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	view, err := series.Read(context.Background(), seriespkg.ReadInput{
		Now: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	episodes := view.Seasons[0].Episodes
	if len(episodes) != 6 {
		t.Fatalf("len(Episodes) = %d, want 6", len(episodes))
	}
	wantStatuses := []seriespkg.EpisodeStatus{
		seriespkg.EpisodeStatusPresent,
		seriespkg.EpisodeStatusMissing,
		seriespkg.EpisodeStatusPending,
		seriespkg.EpisodeStatusUnavailable,
		seriespkg.EpisodeStatusStaged,
		seriespkg.EpisodeStatusStaged,
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

func newReadTestLibrary(t *testing.T, rootPath string) *Library {
	t.Helper()
	root, err := ParseRoot(rootPath)
	if err != nil {
		t.Fatalf("ParseRoot: %v", err)
	}
	idx := NewIndex(root)
	if err := idx.Put(refs.Metadata("tvdb:370070"), mustSeries(t, "Bookworm")); err != nil {
		t.Fatalf("Put index: %v", err)
	}
	return New(root, nil, mediainfo.Inspector{}, idx)
}
