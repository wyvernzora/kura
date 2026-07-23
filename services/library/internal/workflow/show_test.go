package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library/internal/coord"
	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/services/library/internal/domain/series"
	"github.com/wyvernzora/kura/services/library/internal/response"
	"github.com/wyvernzora/kura/services/library/internal/storage/indexfile"
	"github.com/wyvernzora/kura/services/library/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library/internal/textnorm"
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

func TestShow_UsesConfiguredAiringTail(t *testing.T) {
	root := t.TempDir()
	ref, err := refs.ParseSeries("Finale")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ref.String()), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	resolution, err := media.NewResolution(1920, 1080)
	if err != nil {
		t.Fatalf("NewResolution: %v", err)
	}
	activePath := filepath.Join(root, ref.String(), "Season 1", "Finale - S01E01.mkv")
	if err := os.MkdirAll(filepath.Dir(activePath), 0o755); err != nil {
		t.Fatalf("MkdirAll active: %v", err)
	}
	if err := os.WriteFile(activePath, []byte("media"), 0o644); err != nil {
		t.Fatalf("WriteFile active: %v", err)
	}
	mtime := time.Date(2026, 4, 24, 17, 30, 0, 0, time.FixedZone("PDT", -7*60*60))
	rec := &media.Record{
		Path:       activePath,
		Source:     media.SourceWebRip,
		Resolution: resolution,
		Size:       1,
		MTime:      mtime,
	}
	e1, _ := refs.NewEpisode(1, 1)
	e2, _ := refs.NewEpisode(1, 2)
	model := &domainseries.Series{
		Ref:            ref,
		Metadata:       refs.Metadata("tvdb:1"),
		PreferredTitle: textnorm.NFC("Finale"),
		Episodes: map[refs.Episode]domainseries.Episode{
			e1: {AirDate: civil.Date{Year: 2026, Month: 4, Day: 24}, Active: rec},
			e2: {AirDate: civil.Date{Year: 2026, Month: 5, Day: 1}},
		},
	}
	if err := seriesfile.SaveCAS(root, model, coord.NewMutator("test")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	opts := indexfile.DefaultBuildOptions()
	opts.AiringTailDays = 2
	deps := Deps{
		LibRoot:         root,
		Now:             func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
		RowBuildOptions: &opts,
	}
	out, err := Show(context.Background(), deps, ShowInput{Ref: ref})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if out.IsAiring {
		t.Fatal("IsAiring = true, want false with 2-day tail")
	}
	active := out.Seasons[0].Episodes[0].Active
	if active == nil {
		t.Fatal("Active = nil, want media details")
	}
	if active.Dimensions != "1920x1080" {
		t.Errorf("Dimensions = %q, want 1920x1080", active.Dimensions)
	}
	if active.MTime != mtime.UTC().Format(time.RFC3339) {
		t.Errorf("MTime = %q, want %q", active.MTime, mtime.UTC().Format(time.RFC3339))
	}
}

func TestShow_UnknownResolutionAndZeroMTimeAreEmpty(t *testing.T) {
	root := t.TempDir()
	ref := mustShowSeries(t, "Unknown_Media_Details")
	episode := mustShowEpisode(t, 1, 1)
	model := &domainseries.Series{
		Ref:            ref,
		Metadata:       refs.Metadata("tvdb:1"),
		PreferredTitle: textnorm.NFC("Unknown Media Details"),
		Episodes: map[refs.Episode]domainseries.Episode{
			episode: {
				AirDate: civil.Date{Year: 2020, Month: 1, Day: 1},
				Active: &media.Record{
					Path: filepath.Join(
						root,
						ref.String(),
						"Season 1",
						"Unknown Media Details - S01E01.mkv",
					),
					Source: media.SourceUnknown,
				},
			},
		},
	}
	if err := seriesfile.SaveCAS(root, model, coord.NewMutator("test")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	out, err := Show(context.Background(), Deps{LibRoot: root, Now: time.Now}, ShowInput{Ref: ref})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	active := out.Seasons[0].Episodes[0].Active
	if active == nil {
		t.Fatal("Active = nil, want media details")
	}
	if active.Dimensions != "" {
		t.Errorf("Dimensions = %q, want empty", active.Dimensions)
	}
	if active.MTime != "" {
		t.Errorf("MTime = %q, want empty", active.MTime)
	}
}

func TestShow_EpisodeSelectorNoneReturnsEmptySeasons(t *testing.T) {
	root, ref := saveShowSelectorFixture(t)
	selector, err := refs.ParseEpisodeSelector("NONE")
	if err != nil {
		t.Fatalf("ParseEpisodeSelector: %v", err)
	}
	out, err := Show(context.Background(), Deps{
		LibRoot: root,
		Now:     func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
	}, ShowInput{Ref: ref, Episodes: selector})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if out.MetadataRef.String() != "tvdb:1" {
		t.Fatalf("MetadataRef = %s, want tvdb:1", out.MetadataRef)
	}
	if len(out.Seasons) != 0 {
		t.Fatalf("Seasons = %+v, want empty", out.Seasons)
	}
}

func TestShow_EpisodeSelectorAiringSeason(t *testing.T) {
	root, ref := saveShowSelectorFixture(t)
	selector, err := refs.ParseEpisodeSelector("AIRING_SEASON")
	if err != nil {
		t.Fatalf("ParseEpisodeSelector: %v", err)
	}
	out, err := Show(context.Background(), Deps{
		LibRoot: root,
		Now:     func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
	}, ShowInput{Ref: ref, Episodes: selector})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(out.Seasons) != 1 || out.Seasons[0].Number != 2 {
		t.Fatalf("Seasons = %+v, want only S2", out.Seasons)
	}
	if len(out.Seasons[0].Episodes) != 2 {
		t.Fatalf("S2 episodes = %d, want 2", len(out.Seasons[0].Episodes))
	}
}

func TestShow_EpisodeSelectorAiringSeasonComposesWithStatus(t *testing.T) {
	root, ref := saveShowSelectorFixture(t)
	selector, err := refs.ParseEpisodeSelector("AIRING_SEASON")
	if err != nil {
		t.Fatalf("ParseEpisodeSelector: %v", err)
	}
	out, err := Show(context.Background(), Deps{
		LibRoot: root,
		Now:     func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
	}, ShowInput{Ref: ref, Episodes: selector, Status: []response.Status{response.StatusMissing}})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(out.Seasons) != 1 || out.Seasons[0].Number != 2 {
		t.Fatalf("Seasons = %+v, want only S2", out.Seasons)
	}
	eps := out.Seasons[0].Episodes
	if len(eps) != 1 || eps[0].Episode.String() != "S02E0001" || eps[0].Status != response.StatusMissing {
		t.Fatalf("episodes = %+v, want missing S02E0001", eps)
	}
}

func TestShow_EpisodeSelectorAiringSeasonNoMatchReturnsEmptySeasons(t *testing.T) {
	root, ref := saveShowSelectorFixture(t)
	selector, err := refs.ParseEpisodeSelector("AIRING_SEASON")
	if err != nil {
		t.Fatalf("ParseEpisodeSelector: %v", err)
	}
	out, err := Show(context.Background(), Deps{
		LibRoot: root,
		Now:     func() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) },
	}, ShowInput{Ref: ref, Episodes: selector})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(out.Seasons) != 0 {
		t.Fatalf("Seasons = %+v, want empty", out.Seasons)
	}
}

func TestShow_EpisodeSelectorMissingExplicitSeasonStillErrors(t *testing.T) {
	root, ref := saveShowSelectorFixture(t)
	selector, err := refs.ParseEpisodeSelector("S99")
	if err != nil {
		t.Fatalf("ParseEpisodeSelector: %v", err)
	}
	_, err = Show(context.Background(), Deps{
		LibRoot: root,
		Now:     func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
	}, ShowInput{Ref: ref, Episodes: selector})
	if _, ok := errors.AsType[*EpisodeSelectorSeasonMissingError](err); !ok {
		t.Fatalf("err = %v, want EpisodeSelectorSeasonMissingError", err)
	}
}

func saveShowSelectorFixture(t *testing.T) (string, refs.Series) {
	t.Helper()
	root := t.TempDir()
	ref := mustShowSeries(t, "Selector_Show")
	resolution := mustShowResolution(t, 1920, 1080)
	rec := &media.Record{Path: filepath.Join(root, ref.String(), "Season 2", "Selector Show - S02E02.mkv"), Source: media.SourceWebRip, Resolution: resolution, Size: 1}
	e101 := mustShowEpisode(t, 1, 1)
	e201 := mustShowEpisode(t, 2, 1)
	e202 := mustShowEpisode(t, 2, 2)
	model := &domainseries.Series{
		Ref:            ref,
		Metadata:       refs.Metadata("tvdb:1"),
		PreferredTitle: textnorm.NFC("Selector Show"),
		Episodes: map[refs.Episode]domainseries.Episode{
			e101: {AirDate: civil.Date{Year: 2020, Month: 1, Day: 1}, Active: rec},
			e201: {AirDate: civil.Date{Year: 2026, Month: 4, Day: 27}},
			e202: {AirDate: civil.Date{Year: 2026, Month: 5, Day: 7}, Active: rec},
		},
	}
	if err := seriesfile.SaveCAS(root, model, coord.NewMutator("test")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	return root, ref
}

func mustShowSeries(t *testing.T, name string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	return ref
}

func mustShowEpisode(t *testing.T, season, episode int) refs.Episode {
	t.Helper()
	ref, err := refs.NewEpisode(season, episode)
	if err != nil {
		t.Fatalf("NewEpisode: %v", err)
	}
	return ref
}

func mustShowResolution(t *testing.T, width, height int) media.Resolution {
	t.Helper()
	resolution, err := media.NewResolution(width, height)
	if err != nil {
		t.Fatalf("NewResolution: %v", err)
	}
	return resolution
}

func TestUpdateIndexRowPersistsModelSnapshot(t *testing.T) {
	root := t.TempDir()
	ref, err := refs.ParseSeries("Show")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	model := &domainseries.Series{
		Ref:            ref,
		Metadata:       refs.Metadata("tvdb:1"),
		PreferredTitle: textnorm.NFC("Show"),
		Episodes:       map[refs.Episode]domainseries.Episode{},
		LastMutated:    coord.Mutator{Op: "test", PID: 1, Host: "test", At: time.Unix(0, 0).UTC()},
	}
	deps := Deps{
		LibRoot:     root,
		Index:       indexfile.New(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()}),
		Coordinator: coord.NewMCPCoordinator(),
		Now:         time.Now,
	}
	if err := updateIndexModel(context.Background(), deps, model, "test"); err != nil {
		t.Fatalf("updateIndexModel: %v", err)
	}
	loaded, err := indexfile.Load(root, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok, err := loaded.Get(model.Metadata)
	if err != nil || !ok || got != ref {
		t.Fatalf("Get(%s) = %s, %v, %v; want %s, true, nil", model.Metadata, got, ok, err, ref)
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
	if got[0].Path != "series:Season 1/a.mkv" {
		t.Errorf("Path = %q, want series:Season 1/a.mkv", got[0].Path)
	}
	if len(got[0].Companions) != 1 || got[0].Companions[0].Path != "series:Season 1/a.en.srt" {
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
	got := buildStagedExtras("/inbox", items)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].ID != id1.String() {
		t.Errorf("first ID = %s, want %s", got[0].ID, id1)
	}
	if got[0].Path != "inbox:intro.mp4" {
		t.Errorf("Path = %q, want inbox:intro.mp4", got[0].Path)
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
	r4, v4 := mk(3, 1, response.StatusStagedReplacement, "")

	cases := []struct {
		name   string
		filter episodeFilter
		want   []refs.Episode
	}{
		{
			name:   "no filter",
			filter: episodeFilter{},
			want:   []refs.Episode{r1, r2, r3, r4},
		},
		{
			name:   "selector S1",
			filter: episodeFilter{selector: refs.EpisodeSelector{Kind: refs.EpisodeSelectorNormal, Season: 1}},
			want:   []refs.Episode{r1, r2},
		},
		{
			name:   "selector S1E1",
			filter: episodeFilter{selector: refs.EpisodeSelector{Kind: refs.EpisodeSelectorNormal, Season: 1, HasRange: true, From: 1, To: 1}},
			want:   []refs.Episode{r1},
		},
		{
			name:   "status missing",
			filter: episodeFilter{statuses: statusSet([]response.Status{response.StatusMissing})},
			want:   []refs.Episode{r2},
		},
		{
			name:   "status staged includes staged replacement",
			filter: episodeFilter{statuses: statusSet([]response.Status{response.StatusStaged})},
			want:   []refs.Episode{r4},
		},
		{
			name:   "source BluRay drops episodes without active",
			filter: episodeFilter{sources: stringSet([]string{"BluRay"})},
			want:   []refs.Episode{r1},
		},
		{
			name: "compose S1 + present",
			filter: episodeFilter{
				selector: refs.EpisodeSelector{Kind: refs.EpisodeSelectorNormal, Season: 1},
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
			}{{r1, v1}, {r2, v2}, {r3, v3}, {r4, v4}} {
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

func TestComputeEpisodeStatus_NoAirDateIsPending(t *testing.T) {
	now := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	got := computeEpisodeStatus(domainseries.Episode{}, now)
	if got != response.StatusPending {
		t.Fatalf("status = %s, want %s", got, response.StatusPending)
	}

	past := civil.DateOf(now).AddDays(-1)
	got = computeEpisodeStatus(domainseries.Episode{AirDate: past}, now)
	if got != response.StatusMissing {
		t.Fatalf("past status = %s, want %s", got, response.StatusMissing)
	}
}

func TestBuildStagedExtras_MissingSourceLeavesIsDirFalse(t *testing.T) {
	id := ulid.MustParse("01H0000000000000000000AAAA")
	items := []domainseries.StagedExtraItem{
		{ID: id, Season: 1, Path: "/inbox/nonexistent/path"},
	}
	got := buildStagedExtras("/inbox", items)
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].IsDir {
		t.Errorf("missing source IsDir = true, want false")
	}
}
