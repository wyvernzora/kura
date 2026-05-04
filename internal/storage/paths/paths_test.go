package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

func TestLibraryAndSeriesPaths(t *testing.T) {
	root := "/library"
	ref, err := refs.ParseSeries("Bookworm")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"LibraryKuraDir", paths.LibraryKuraDir(root), filepath.Join("/library", ".kura")},
		{"IndexFile", paths.IndexFile(root), filepath.Join("/library", ".kura", "index.jsonl")},
		{"LegacyIndexFile", paths.LegacyIndexFile(root), filepath.Join("/library", ".kura", "index.tsv")},
		{"SeriesDir", paths.SeriesDir(root, ref), filepath.Join("/library", "Bookworm")},
		{"SeriesKuraDir", paths.SeriesKuraDir(root, ref), filepath.Join("/library", "Bookworm", ".kura")},
		{"SeriesMetadata", paths.SeriesMetadata(root, ref), filepath.Join("/library", "Bookworm", ".kura", "series.json")},
		{"TrashDir", paths.TrashDir(root, ref), filepath.Join("/library", "Bookworm", ".kura", "trash")},
		{"TrashEntry", paths.TrashEntry(root, ref, "01H"), filepath.Join("/library", "Bookworm", ".kura", "trash", "01H")},
		{"TrashMeta", paths.TrashMeta(root, ref, "01H"), filepath.Join("/library", "Bookworm", ".kura", "trash", "01H", "meta.json")},
		{"TrashMedia", paths.TrashMedia(root, ref, "01H", "old.mkv"), filepath.Join("/library", "Bookworm", ".kura", "trash", "01H", "old.mkv")},
		{"PlanDir", paths.PlanDir(root, ref), filepath.Join("/library", "Bookworm", ".kura", "reconcile")},
		{"PlanFile", paths.PlanFile(root, ref, "01H"), filepath.Join("/library", "Bookworm", ".kura", "reconcile", "01H.jsonl")},
		{"SeasonDir/1", paths.SeasonDir(root, ref, 1), filepath.Join("/library", "Bookworm", "Season 1")},
		{"SeasonDir/0", paths.SeasonDir(root, ref, 0), filepath.Join("/library", "Bookworm")},
		{"SeasonExtraDir", paths.SeasonExtraDir(root, ref, 2), filepath.Join("/library", "Bookworm", "Season 2", "Extra")},
		{"EpisodeMedia", paths.EpisodeMedia(root, ref, 1, "ep.mkv"), filepath.Join("/library", "Bookworm", "Season 1", "ep.mkv")},
		{"EpisodeMedia/0", paths.EpisodeMedia(root, ref, 0, "special.mkv"), filepath.Join("/library", "Bookworm", "special.mkv")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestRelHelpers(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"TrashRel", paths.TrashRel("01H", "old.mkv"), ".kura/trash/01H/old.mkv"},
		{"EpisodeMediaRel/1", paths.EpisodeMediaRel(1, "ep.mkv"), "Season 1/ep.mkv"},
		{"EpisodeMediaRel/0", paths.EpisodeMediaRel(0, "special.mkv"), "special.mkv"},
		{"EpisodeMediaRel/12", paths.EpisodeMediaRel(12, "ep.mkv"), "Season 12/ep.mkv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}
