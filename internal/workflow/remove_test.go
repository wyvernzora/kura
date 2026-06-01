package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

func TestEstimatedRemoveBytesPurgeStatsKnownFilesOnly(t *testing.T) {
	root := t.TempDir()
	ref, err := refs.ParseSeries("Show")
	if err != nil {
		t.Fatal(err)
	}
	ep, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	active := writeSizedFile(t, paths.EpisodeMedia(root, ref, 1, "show.s01e01.mkv"), 7)
	companion := writeSizedFile(t, paths.EpisodeMedia(root, ref, 1, "show.s01e01.ass"), 5)
	writeSizedFile(t, filepath.Join(paths.SeriesDir(root, ref), "loose-upgrade.mkv"), 11)
	model := &domainseries.Series{
		Ref: ref,
		Episodes: map[refs.Episode]domainseries.Episode{
			ep: {
				Active: &media.Record{
					Path: active,
					Companions: []media.Companion{{
						Path: companion,
					}},
				},
			},
		},
	}

	if got := estimatedRemoveBytes(root, ref, true, model); got != 12 {
		t.Fatalf("estimatedRemoveBytes = %d, want 12", got)
	}
}

func writeSizedFile(t *testing.T, path string, size int) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
