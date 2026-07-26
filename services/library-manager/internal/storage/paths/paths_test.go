package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
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
		{"LibraryKuraDir", paths.LibraryKuraDir(root), filepath.Join("/library", ".kura")},                                                       //nolint:gocritic // test fixture root
		{"IndexFile", paths.IndexFile(root), filepath.Join("/library", ".kura", "index.jsonl")},                                                  //nolint:gocritic // test fixture root
		{"LegacyIndexFile", paths.LegacyIndexFile(root), filepath.Join("/library", ".kura", "index.tsv")},                                        //nolint:gocritic // test fixture root
		{"SeriesDir", paths.SeriesDir(root, ref), filepath.Join("/library", "Bookworm")},                                                         //nolint:gocritic // test fixture root
		{"SeriesKuraDir", paths.SeriesKuraDir(root, ref), filepath.Join("/library", "Bookworm", ".kura")},                                        //nolint:gocritic // test fixture root
		{"SeriesMetadata", paths.SeriesMetadata(root, ref), filepath.Join("/library", "Bookworm", ".kura", "series.json")},                       //nolint:gocritic // test fixture root
		{"TrashDir", paths.TrashDir(root, ref), filepath.Join("/library", "Bookworm", ".kura", "trash")},                                         //nolint:gocritic // test fixture root
		{"TrashEntry", paths.TrashEntry(root, ref, "01H"), filepath.Join("/library", "Bookworm", ".kura", "trash", "01H")},                       //nolint:gocritic // test fixture root
		{"TrashMeta", paths.TrashMeta(root, ref, "01H"), filepath.Join("/library", "Bookworm", ".kura", "trash", "01H", "meta.json")},            //nolint:gocritic // test fixture root
		{"TrashMedia", paths.TrashMedia(root, ref, "01H", "old.mkv"), filepath.Join("/library", "Bookworm", ".kura", "trash", "01H", "old.mkv")}, //nolint:gocritic // test fixture root
		{"PlanDir", paths.PlanDir(root, ref), filepath.Join("/library", "Bookworm", ".kura", "reconcile")},                                       //nolint:gocritic // test fixture root
		{"PlanFile", paths.PlanFile(root, ref, "01H"), filepath.Join("/library", "Bookworm", ".kura", "reconcile", "01H.jsonl")},                 //nolint:gocritic // test fixture root
		{"SeasonDir/1", paths.SeasonDir(root, ref, 1), filepath.Join("/library", "Bookworm", "Season 1")},                                        //nolint:gocritic // test fixture root
		{"SeasonDir/0", paths.SeasonDir(root, ref, 0), filepath.Join("/library", "Bookworm")},                                                    //nolint:gocritic // test fixture root
		{"SeasonExtraDir", paths.SeasonExtraDir(root, ref, 2), filepath.Join("/library", "Bookworm", "Season 2", "Extra")},                       //nolint:gocritic // test fixture root
		{"EpisodeMedia", paths.EpisodeMedia(root, ref, 1, "ep.mkv"), filepath.Join("/library", "Bookworm", "Season 1", "ep.mkv")},                //nolint:gocritic // test fixture root
		{"EpisodeMedia/0", paths.EpisodeMedia(root, ref, 0, "special.mkv"), filepath.Join("/library", "Bookworm", "special.mkv")},                //nolint:gocritic // test fixture root
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
		{"SeasonExtraRel/0", paths.SeasonExtraRel(0), "Extra/"},
		{"SeasonExtraRel/3", paths.SeasonExtraRel(3), "Season 3/Extra/"},
		{"ExtraRel/no-prefix", paths.ExtraRel(1, "", "bts.mp4"), "Season 1/Extra/bts.mp4"},
		{"ExtraRel/with-prefix", paths.ExtraRel(2, "behind-the-scenes", "bts.mp4"), "Season 2/Extra/behind-the-scenes/bts.mp4"},
		{"ExtraRel/season-0", paths.ExtraRel(0, "scans", "scan.zip"), "Extra/scans/scan.zip"},
		{"ExtraRel/dir-basename", paths.ExtraRel(1, "", "specials-folder"), "Season 1/Extra/specials-folder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}
