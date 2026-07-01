package indexfile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloud.google.com/go/civil"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func mustEpisode(t *testing.T, season, ep int) refs.Episode {
	t.Helper()
	r, err := refs.NewEpisode(season, ep)
	if err != nil {
		t.Fatalf("NewEpisode: %v", err)
	}
	return r
}

func mustResolution(t *testing.T, w, h int) media.Resolution {
	t.Helper()
	r, err := media.NewResolution(w, h)
	if err != nil {
		t.Fatalf("NewResolution: %v", err)
	}
	return r
}

func TestBuildRowFromModel_EmptyEpisodes(t *testing.T) {
	model := &series.Series{
		Ref:            mustParseSeries(t, "Bookworm"),
		Metadata:       refs.Metadata("tvdb:370070"),
		PreferredTitle: textnorm.NFC("Bookworm"),
		Episodes:       map[refs.Episode]series.Episode{},
	}
	row := indexfile.BuildRowFromModel(model, time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC))
	if row.Series != model.Ref {
		t.Fatalf("Series = %s, want %s", row.Series, model.Ref)
	}
	if row.Status != response.ListStatusIncomplete {
		t.Fatalf("Status = %s, want incomplete (no episodes)", row.Status)
	}
	if row.EpisodeCount != 0 || row.SeasonCount != 0 {
		t.Fatalf("counts non-zero: %+v", row)
	}
	if row.UpdatedAt == "" {
		t.Fatal("UpdatedAt empty")
	}
}

func TestBuildRowFromModel_AllPendingIsComplete(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		episodes map[refs.Episode]series.Episode
	}{
		{
			name: "future dated",
			episodes: map[refs.Episode]series.Episode{
				mustEpisode(t, 1, 1): {AirDate: civil.Date{Year: 2099, Month: 1, Day: 1}},
				mustEpisode(t, 1, 2): {AirDate: civil.Date{Year: 2099, Month: 1, Day: 8}},
			},
		},
		{
			name: "tba placeholders",
			episodes: map[refs.Episode]series.Episode{
				mustEpisode(t, 1, 1): {},
				mustEpisode(t, 1, 2): {},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &series.Series{
				Ref:      mustParseSeries(t, "Show"),
				Metadata: refs.Metadata("tvdb:1"),
				Episodes: tt.episodes,
			}
			row := indexfile.BuildRowFromModel(model, now)
			if row.Status != response.ListStatusComplete {
				t.Fatalf("Status = %s, want complete (all episodes pending)", row.Status)
			}
			if row.EpisodeCount != 0 || row.EpisodesAvailable != 0 {
				t.Fatalf("episode counts = %d/%d, want 0/0", row.EpisodesAvailable, row.EpisodeCount)
			}
		})
	}
}

func TestBuildRowFromModel_AllActiveIsComplete(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{
		Source:     media.SourceBluRay,
		Resolution: mustResolution(t, 1920, 1080),
		Size:       100,
	}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {Active: rec},
			mustEpisode(t, 1, 2): {Active: rec},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.Status != response.ListStatusComplete {
		t.Fatalf("Status = %s, want complete", row.Status)
	}
	if row.EpisodeCount != 2 || row.EpisodesAvailable != 2 {
		t.Fatalf("episode counts = %d/%d, want 2/2", row.EpisodesAvailable, row.EpisodeCount)
	}
	if row.SeasonCount != 1 || row.SeasonsAvailable != 1 {
		t.Fatalf("season counts = %d/%d, want 1/1", row.SeasonsAvailable, row.SeasonCount)
	}
	if len(row.Resolutions) != 1 || row.Resolutions[0] != "1080p" {
		t.Fatalf("Resolutions = %v, want [1080p]", row.Resolutions)
	}
	if len(row.Sources) != 1 || row.Sources[0] != "BluRay" {
		t.Fatalf("Sources = %v, want [BluRay]", row.Sources)
	}
}

func TestBuildRowFromModel_StagedFlag(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {Staged: &media.Record{Source: media.SourceBluRay, Resolution: mustResolution(t, 1280, 720), Size: 1}},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if !row.Staged {
		t.Fatal("Staged = false, want true")
	}
}

// Single-cour weekly run with a future episode within cadence: cour 1
// has E1 (2 weeks ago) and E2 (3 days out). Cour 1's first date is in
// the past, last date is in the future → airing.
func TestBuildRowFromModel_AiringFlag_WeeklyCadence(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	past := civil.Date{Year: 2026, Month: 4, Day: 27}
	near := civil.Date{Year: 2026, Month: 5, Day: 7}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {AirDate: past, Active: rec},
			mustEpisode(t, 1, 2): {AirDate: near},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.Status != response.ListStatusComplete {
		t.Fatalf("Status = %s, want complete (no missing past eps)", row.Status)
	}
	if !row.IsAiring {
		t.Fatal("IsAiring = false, want true (cour has past first + future last)")
	}
}

func TestBuildRowFromModel_AiringFlag_FirstEpFarFuture(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	farFuture1 := civil.Date{Year: 2099, Month: 1, Day: 1}
	farFuture2 := civil.Date{Year: 2099, Month: 1, Day: 8}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {AirDate: farFuture1},
			mustEpisode(t, 1, 2): {AirDate: farFuture2},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.IsAiring {
		t.Fatal("IsAiring = true, want false (first ep beyond 168h horizon)")
	}
}

func TestBuildRowFromModel_AiringFlag_FirstEpWithinWindow(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	withinWindow := civil.Date{Year: 2026, Month: 5, Day: 8} // 4 days ahead
	farFuture := civil.Date{Year: 2099, Month: 1, Day: 1}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {AirDate: withinWindow},
			mustEpisode(t, 1, 2): {AirDate: farFuture},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if !row.IsAiring {
		t.Fatal("IsAiring = false, want true (first ep airs within 168h)")
	}
}

func TestBuildRowFromModel_AiringFlag_NoFutureEpisodes(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	past := civil.Date{Year: 2024, Month: 6, Day: 1}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {AirDate: past, Active: rec},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.IsAiring {
		t.Fatal("IsAiring = true, want false (no future eps)")
	}
}

func TestBuildRowFromModel_AiringFlag_TailWindow(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Finale"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {AirDate: civil.Date{Year: 2026, Month: 4, Day: 24}, Active: rec},
			mustEpisode(t, 1, 2): {AirDate: civil.Date{Year: 2026, Month: 5, Day: 1}},
		},
	}
	if row := indexfile.BuildRowFromModel(model, now); !row.IsAiring {
		t.Fatal("default tail IsAiring = false, want true")
	}
	opts := indexfile.DefaultBuildOptions()
	opts.AiringTailDays = 2
	if row := indexfile.BuildRowFromModelWithOptions(model, now, opts); row.IsAiring {
		t.Fatal("2-day tail IsAiring = true, want false")
	}
}

func TestBuildRowFromModel_AiringFlag_ZeroTail(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	opts := indexfile.DefaultBuildOptions()
	opts.AiringTailDays = 0

	todayModel := &series.Series{
		Ref:      mustParseSeries(t, "Today"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {AirDate: civil.Date{Year: 2026, Month: 5, Day: 4}, Active: rec},
		},
	}
	if row := indexfile.BuildRowFromModelWithOptions(todayModel, now, opts); !row.IsAiring {
		t.Fatal("same-day finale IsAiring = false, want true with zero tail")
	}

	yesterdayModel := &series.Series{
		Ref:      mustParseSeries(t, "Yesterday"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {AirDate: civil.Date{Year: 2026, Month: 5, Day: 3}, Active: rec},
		},
	}
	if row := indexfile.BuildRowFromModelWithOptions(yesterdayModel, now, opts); row.IsAiring {
		t.Fatal("yesterday finale IsAiring = true, want false with zero tail")
	}
}

// Helmode-style: cour 1 is fully aired (Jan-Mar 2026, weekly); E13
// jumps to July 2026 — a split-cour gap of ~64 days. Cour 2 contains
// only E13, whose first air date is beyond the 7d horizon, so the
// series should not be flagged airing on May 4 2026.
func TestBuildRowFromModel_AiringFlag_SplitCourHiatus(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	cour1Start := civil.Date{Year: 2026, Month: 1, Day: 10}
	cour2 := civil.Date{Year: 2026, Month: 7, Day: 11}
	episodes := map[refs.Episode]series.Episode{}
	for i := 1; i <= 12; i++ {
		air := cour1Start.AddDays(7 * (i - 1))
		episodes[mustEpisode(t, 1, i)] = series.Episode{AirDate: air, Active: rec}
	}
	episodes[mustEpisode(t, 1, 13)] = series.Episode{AirDate: cour2}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Helmode"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: episodes,
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.IsAiring {
		t.Fatal("IsAiring = true, want false (cour 1 ended in March, cour 2 first ep is 64d out)")
	}
}

// Active cour mid-run: cour 1 ran Jan-Mar (all aired), cour 2 started
// Apr 25 with weekly cadence; today is May 4, next pending is May 9.
// Cour 2's first date (Apr 25) is in the past, last date (May 16) is
// in the future → cour 2 qualifies, series airing.
func TestBuildRowFromModel_AiringFlag_ActiveSecondCour(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	cour1Start := civil.Date{Year: 2026, Month: 1, Day: 10}
	cour2Start := civil.Date{Year: 2026, Month: 4, Day: 25}
	episodes := map[refs.Episode]series.Episode{}
	for i := 1; i <= 12; i++ {
		air := cour1Start.AddDays(7 * (i - 1))
		episodes[mustEpisode(t, 1, i)] = series.Episode{AirDate: air, Active: rec}
	}
	// cour 2: E13 already aired (Apr 25), E14-E16 pending weekly.
	episodes[mustEpisode(t, 1, 13)] = series.Episode{AirDate: cour2Start, Active: rec}
	for i := 14; i <= 16; i++ {
		air := cour2Start.AddDays(7 * (i - 13))
		episodes[mustEpisode(t, 1, i)] = series.Episode{AirDate: air}
	}
	model := &series.Series{
		Ref:      mustParseSeries(t, "TwoCour"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: episodes,
	}
	row := indexfile.BuildRowFromModel(model, now)
	if !row.IsAiring {
		t.Fatal("IsAiring = false, want true (cour 2 started Apr 25, has future eps)")
	}
}

// Regression: pending (unaired) episodes must not contribute to
// EpisodeCount or SeasonCount. Re:Zero-shaped scenario — a series
// whose latest season has 5 aired-missing eps and 14 announced-but-
// unaired eps should report 66/71 (not 66/85). SeasonCount should
// include the partial season because it has aired episodes (5
// missing). A pure future-only season is excluded.
func TestBuildRowFromModel_PendingExcludedFromTotals(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	pastBase := civil.Date{Year: 2024, Month: 1, Day: 1}
	futureBase := civil.Date{Year: 2026, Month: 5, Day: 11}
	farFutureBase := civil.Date{Year: 2027, Month: 1, Day: 1}
	episodes := map[refs.Episode]series.Episode{}
	// Seasons 1-3: 22 active each = 66 aired-present.
	for season := 1; season <= 3; season++ {
		for i := 1; i <= 22; i++ {
			ep := mustEpisode(t, season, i)
			episodes[ep] = series.Episode{AirDate: pastBase.AddDays(7 * i), Active: rec}
		}
	}
	// Season 4: 5 aired-missing (no record), 14 announced-but-unaired.
	for i := 1; i <= 5; i++ {
		ep := mustEpisode(t, 4, i)
		episodes[ep] = series.Episode{AirDate: pastBase.AddDays(7 * i)}
	}
	for i := 6; i <= 19; i++ {
		ep := mustEpisode(t, 4, i)
		episodes[ep] = series.Episode{AirDate: futureBase.AddDays(7 * (i - 6))}
	}
	// Season 5: 100% future. Must not bump SeasonCount or EpisodeCount.
	for i := 1; i <= 13; i++ {
		ep := mustEpisode(t, 5, i)
		episodes[ep] = series.Episode{AirDate: farFutureBase.AddDays(7 * i)}
	}

	model := &series.Series{
		Ref:      mustParseSeries(t, "Re_Zero"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: episodes,
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.EpisodeCount != 71 {
		t.Fatalf("EpisodeCount = %d, want 71 (66 active + 5 aired-missing; 14 unaired in season 4 + 13 unaired in season 5 must be excluded)", row.EpisodeCount)
	}
	if row.EpisodesAvailable != 66 {
		t.Fatalf("EpisodesAvailable = %d, want 66", row.EpisodesAvailable)
	}
	if row.SeasonCount != 4 {
		t.Fatalf("SeasonCount = %d, want 4 (seasons 1-4 have aired eps; season 5 is future-only)", row.SeasonCount)
	}
	if row.SeasonsAvailable != 3 {
		t.Fatalf("SeasonsAvailable = %d, want 3 (seasons 1-3 have active records)", row.SeasonsAvailable)
	}
	if row.Status != response.ListStatusIncomplete {
		t.Fatalf("Status = %s, want incomplete (5 aired-missing eps)", row.Status)
	}
}

// Regression: a TBA placeholder episode (no AirDate, no record) is the
// strongest form of pending and must not flip the series to incomplete.
// Kusuriya-shaped scenario — S1+S2 fully present, S3 announced with a
// single TBA episode should stay Complete with EpisodeCount unchanged.
func TestBuildRowFromModel_TBAPlaceholderExcluded(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	pastBase := civil.Date{Year: 2024, Month: 1, Day: 1}
	episodes := map[refs.Episode]series.Episode{}
	for season := 1; season <= 2; season++ {
		for i := 1; i <= 12; i++ {
			ep := mustEpisode(t, season, i)
			episodes[ep] = series.Episode{AirDate: pastBase.AddDays(7 * i), Active: rec}
		}
	}
	// S3 placeholder: TBA episode with no AirDate and no record.
	episodes[mustEpisode(t, 3, 1)] = series.Episode{}

	model := &series.Series{
		Ref:      mustParseSeries(t, "Kusuriya"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: episodes,
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.Status != response.ListStatusComplete {
		t.Fatalf("Status = %s, want complete (TBA placeholder must not count as missing)", row.Status)
	}
	if row.EpisodeCount != 24 || row.EpisodesAvailable != 24 {
		t.Fatalf("episode counts = %d/%d, want 24/24", row.EpisodesAvailable, row.EpisodeCount)
	}
	if row.SeasonCount != 2 || row.SeasonsAvailable != 2 {
		t.Fatalf("season counts = %d/%d, want 2/2 (S3 is TBA-only)", row.SeasonsAvailable, row.SeasonCount)
	}
}

func TestBuildRowFromModel_SpecialsExcluded(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceBluRay, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 0, 1): {Active: rec}, // special; must not count
			mustEpisode(t, 1, 1): {Active: rec},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.EpisodeCount != 1 || row.SeasonCount != 1 {
		t.Fatalf("counts = %d eps / %d seasons, want 1/1 (specials excluded)", row.EpisodeCount, row.SeasonCount)
	}
}

func TestBuildRow_UntrackedDir(t *testing.T) {
	libRoot := t.TempDir()
	ref := mustParseSeries(t, "Untracked")
	if err := os.MkdirAll(filepath.Join(libRoot, ref.String()), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	row, err := indexfile.BuildRow(libRoot, ref, time.Now().UTC())
	if err != nil {
		t.Fatalf("BuildRow: %v", err)
	}
	if row.Status != response.ListStatusUntracked {
		t.Fatalf("Status = %s, want untracked", row.Status)
	}
	if row.Title != ref.String() {
		t.Fatalf("Title = %q, want %q", row.Title, ref.String())
	}
	if row.Metadata != "" {
		t.Fatalf("Metadata = %q, want empty for untracked", row.Metadata)
	}
}

func TestBuildRow_LoadedFromDisk(t *testing.T) {
	libRoot := t.TempDir()
	ref := mustParseSeries(t, "Bookworm")
	model := &series.Series{
		Ref:            ref,
		Metadata:       refs.Metadata("tvdb:370070"),
		PreferredTitle: textnorm.NFC("Ascendance of a Bookworm"),
		CanonicalTitle: textnorm.NFC("Honzuki no Gekokujou"),
		Episodes:       map[refs.Episode]series.Episode{},
	}
	if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("test")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}
	row, err := indexfile.BuildRow(libRoot, ref, time.Now().UTC())
	if err != nil {
		t.Fatalf("BuildRow: %v", err)
	}
	if row.Title != "Ascendance of a Bookworm" {
		t.Fatalf("Title = %q, want preferred title", row.Title)
	}
	if row.CanonicalTitle != "Honzuki no Gekokujou" {
		t.Fatalf("CanonicalTitle = %q", row.CanonicalTitle)
	}
	if row.Metadata != "tvdb:370070" {
		t.Fatalf("Metadata = %q", row.Metadata)
	}
}

func TestRebuild_IncludesUntrackedAndSkipsDotDirs(t *testing.T) {
	libRoot := t.TempDir()
	for _, name := range []string{"Bookworm", "Untracked", ".kura", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(libRoot, name), 0o755); err != nil {
			t.Fatalf("Mkdir %s: %v", name, err)
		}
	}
	ref := mustParseSeries(t, "Bookworm")
	model := &series.Series{
		Ref:            ref,
		Metadata:       refs.Metadata("tvdb:370070"),
		PreferredTitle: textnorm.NFC("Bookworm"),
		Episodes:       map[refs.Episode]series.Episode{},
	}
	if err := seriesfile.SaveCAS(libRoot, model, coord.NewMutator("test")); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}

	idx, err := indexfile.Rebuild(context.Background(), libRoot, indexfile.BuildRow)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	rows := idx.Rows()
	if len(rows) != 2 {
		t.Fatalf("Rebuild rows = %d, want 2 (Bookworm + Untracked)", len(rows))
	}

	got := map[string]response.ListStatus{}
	for _, row := range rows {
		got[row.Series.String()] = row.Status
	}
	if got["Bookworm"] != response.ListStatusIncomplete {
		t.Fatalf("Bookworm Status = %s, want incomplete (empty model)", got["Bookworm"])
	}
	if got["Untracked"] != response.ListStatusUntracked {
		t.Fatalf("Untracked Status = %s, want untracked", got["Untracked"])
	}
	if _, ok := got[".kura"]; ok {
		t.Fatal(".kura should be skipped")
	}
	if _, ok := got[".hidden"]; ok {
		t.Fatal("dotfiles should be skipped")
	}
}

func TestIndex_GetRowAndOrderedSeries(t *testing.T) {
	idx := indexfile.New(t.TempDir())
	now := time.Now().UTC()
	rows := []indexfile.Row{
		{Series: mustParseSeries(t, "Zebra"), Metadata: refs.Metadata("tvdb:3"), Title: "Zebra", Status: response.ListStatusComplete, UpdatedAt: now.Format(time.RFC3339)},
		{Series: mustParseSeries(t, "Apple"), Metadata: refs.Metadata("tvdb:1"), Title: "Apple", Status: response.ListStatusComplete, UpdatedAt: now.Format(time.RFC3339)},
		{Series: mustParseSeries(t, "Mango"), Metadata: refs.Metadata("tvdb:2"), Title: "Mango", Status: response.ListStatusComplete, UpdatedAt: now.Format(time.RFC3339)},
	}
	for _, row := range rows {
		if err := idx.Upsert(row); err != nil {
			t.Fatalf("Upsert(%s): %v", row.Series, err)
		}
	}

	// Order is alphabetical by title.
	ordered := idx.OrderedSeries()
	if len(ordered) != 3 {
		t.Fatalf("OrderedSeries length = %d, want 3", len(ordered))
	}
	want := []string{"Apple", "Mango", "Zebra"}
	for i, ref := range ordered {
		if ref.String() != want[i] {
			t.Fatalf("ordered[%d] = %s, want %s", i, ref, want[i])
		}
	}

	// GetRow returns the same row we upserted.
	got, ok := idx.GetRow(mustParseSeries(t, "Mango"))
	if !ok || got.Title != "Mango" || got.Metadata != "tvdb:2" {
		t.Fatalf("GetRow(Mango) = (%+v, %v)", got, ok)
	}

	// Get by metadata still works (selector lookup).
	gotSeries, ok, err := idx.Get(refs.Metadata("tvdb:1"))
	if err != nil || !ok || gotSeries.String() != "Apple" {
		t.Fatalf("Get(tvdb:1) = (%s, %v, %v)", gotSeries, ok, err)
	}
}

func TestIndex_RemoveDropsBothMaps(t *testing.T) {
	idx := indexfile.New(t.TempDir())
	row := indexfile.Row{
		Series:   mustParseSeries(t, "Bookworm"),
		Metadata: refs.Metadata("tvdb:370070"),
		Title:    "Bookworm",
		Status:   response.ListStatusComplete,
	}
	if err := idx.Upsert(row); err != nil {
		t.Fatal(err)
	}
	idx.Remove(row.Series)
	if _, ok := idx.GetRow(row.Series); ok {
		t.Fatal("GetRow after Remove should be absent")
	}
	if _, ok, _ := idx.Get(row.Metadata); ok {
		t.Fatal("Get(metadata) after Remove should be absent")
	}
}
