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

func TestBuildRowFromModel_PendingIsAiring(t *testing.T) {
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	rec := &media.Record{Source: media.SourceWebRip, Resolution: mustResolution(t, 1920, 1080), Size: 1}
	future := civil.Date{Year: 2099, Month: 1, Day: 1}
	model := &series.Series{
		Ref:      mustParseSeries(t, "Show"),
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			mustEpisode(t, 1, 1): {Active: rec},
			mustEpisode(t, 1, 2): {AirDate: future},
		},
	}
	row := indexfile.BuildRowFromModel(model, now)
	if row.Status != response.ListStatusAiring {
		t.Fatalf("Status = %s, want airing (one pending)", row.Status)
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
