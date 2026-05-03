package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
)

func TestShow_PrecancelledCtxReturnsEarly(t *testing.T) {
	ref, err := refs.ParseSeries("AnyShow")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := Deps{LibRoot: t.TempDir(), Now: time.Now}
	_, err = Show(ctx, deps, ShowInput{Ref: ref})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestBuildStagedTrash_EmptyReturnsNil(t *testing.T) {
	if got := buildStagedTrash("/lib/Show", nil); got != nil {
		t.Fatalf("buildStagedTrash(nil) = %v, want nil", got)
	}
}

func TestBuildStagedTrash_RelativizesAndSortsByULID(t *testing.T) {
	id1 := ulid.MustParse("01H0000000000000000000AAAA")
	id2 := ulid.MustParse("01H0000000000000000000BBBB")
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	items := []domainseries.StagedTrashItem{
		// added in reverse order; output should sort by ULID asc.
		{
			ID:    id2,
			Path:  "/lib/Show/Season 1/b.mkv",
			Size:  200,
			MTime: now,
		},
		{
			ID:    id1,
			Path:  "/lib/Show/Season 1/a.mkv",
			Size:  100,
			MTime: now,
			Companions: []media.Companion{
				{Path: "/lib/Show/Season 1/a.en.srt", Size: 5, MTime: now},
			},
		},
	}
	got := buildStagedTrash("/lib/Show", items)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != id1.String() {
		t.Errorf("first ID = %s, want %s (sorted)", got[0].ID, id1)
	}
	if got[0].Path != "Season 1/a.mkv" {
		t.Errorf("Path = %q, want series-relative", got[0].Path)
	}
	if len(got[0].Companions) != 1 || got[0].Companions[0].Path != "Season 1/a.en.srt" {
		t.Errorf("Companions = %+v", got[0].Companions)
	}
}

func TestBuildStagedExtras_ReadsPersistedIsDir(t *testing.T) {
	id1 := ulid.MustParse("01H0000000000000000000AAAA")
	id2 := ulid.MustParse("01H0000000000000000000BBBB")
	items := []domainseries.StagedExtraItem{
		{ID: id2, Season: 2, Path: "/inbox/bts", IsDir: true},
		{ID: id1, Season: 1, Path: "/inbox/intro.mp4", IsDir: false},
	}
	got := buildStagedExtras(items)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].ID != id1.String() {
		t.Errorf("first ID = %s, want %s", got[0].ID, id1)
	}
	if got[0].IsDir {
		t.Errorf("file extra IsDir = true; expected false (persisted value)")
	}
	if !got[1].IsDir {
		t.Errorf("dir extra IsDir = false; expected true (persisted value)")
	}
}

func TestEpisodeFilter_SelectorAndStatus(t *testing.T) {
	mk := func(season, ep int, st response.Status, source string) (refs.Episode, response.EpisodeShow) {
		ref, _ := refs.NewEpisode(season, ep)
		view := response.EpisodeShow{Episode: ref, Status: st}
		if source != "" {
			view.Active = &response.MediaShow{Source: source, Resolution: "1080p"}
		}
		return ref, view
	}
	r1, v1 := mk(1, 1, response.StatusPresent, "BluRay")
	r2, v2 := mk(1, 2, response.StatusMissing, "")
	r3, v3 := mk(2, 1, response.StatusPresent, "WebRip")

	cases := []struct {
		name   string
		filter episodeFilter
		want   []refs.Episode
	}{
		{
			name:   "no filter",
			filter: episodeFilter{},
			want:   []refs.Episode{r1, r2, r3},
		},
		{
			name:   "selector S1",
			filter: episodeFilter{selector: refs.EpisodeSelector{Active: true, Season: 1}},
			want:   []refs.Episode{r1, r2},
		},
		{
			name:   "selector S1E1",
			filter: episodeFilter{selector: refs.EpisodeSelector{Active: true, Season: 1, HasRange: true, From: 1, To: 1}},
			want:   []refs.Episode{r1},
		},
		{
			name:   "status missing",
			filter: episodeFilter{statuses: statusSet([]response.Status{response.StatusMissing})},
			want:   []refs.Episode{r2},
		},
		{
			name:   "source BluRay drops episodes without active",
			filter: episodeFilter{sources: stringSet([]string{"BluRay"})},
			want:   []refs.Episode{r1},
		},
		{
			name: "compose S1 + present",
			filter: episodeFilter{
				selector: refs.EpisodeSelector{Active: true, Season: 1},
				statuses: statusSet([]response.Status{response.StatusPresent}),
			},
			want: []refs.Episode{r1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := []refs.Episode{}
			for _, pair := range []struct {
				ref  refs.Episode
				view response.EpisodeShow
			}{{r1, v1}, {r2, v2}, {r3, v3}} {
				if tc.filter.match(pair.ref, pair.view) {
					got = append(got, pair.ref)
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d] = %s, want %s", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestBuildStagedExtras_MissingSourceLeavesIsDirFalse(t *testing.T) {
	id := ulid.MustParse("01H0000000000000000000AAAA")
	items := []domainseries.StagedExtraItem{
		{ID: id, Season: 1, Path: "/nonexistent/path"},
	}
	got := buildStagedExtras(items)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].IsDir {
		t.Errorf("missing source IsDir = true, want false")
	}
}
