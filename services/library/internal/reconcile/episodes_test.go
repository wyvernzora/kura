package reconcile

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/textnorm"
)

const testToken = "0123456789ab"

func mustEpisode(t *testing.T, season, ep int) refs.Episode {
	t.Helper()
	r, err := refs.NewEpisode(season, ep)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPlanEpisodes_StagedAddEmitsPrimaryFileMove(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Show")
	e1 := mustEpisode(t, 1, 1)
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			e1: {Staged: &media.Record{Path: "/inbox/winner.mkv", Source: media.ParseSource("BluRay")}},
		},
	}
	steps, err := planEpisodes(testToken, seriesRef, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	step := steps[0]
	if step.Kind != StepFileMove {
		t.Errorf("kind = %q", step.Kind)
	}
	if step.Owner.Kind != OwnerEpisode || step.Owner.EpisodeIntent != "add" {
		t.Errorf("owner = %+v, want episode/add", step.Owner)
	}
	if step.From != "/inbox/winner.mkv" {
		t.Errorf("From = %q", step.From)
	}
	if step.To == "" {
		t.Error("To empty")
	}
}

func TestPlanEpisodes_StagedReplaceEmitsTrashThenStage(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Show")
	e1 := mustEpisode(t, 1, 1)
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			e1: {
				Active: &media.Record{
					Path:   "/lib/Show/Season 1/old.mkv",
					Source: media.ParseSource("WebRip"),
					// Resolution intentionally zero; planner doesn't need it for replace.
					Size:  12345,
					MTime: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				},
				Staged: &media.Record{Path: "/inbox/new.mkv", Source: media.ParseSource("BluRay")},
			},
		},
	}
	steps, err := planEpisodes(testToken, seriesRef, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2 (trash + stage)", len(steps))
	}
	if steps[0].Owner.Kind != OwnerTrash {
		t.Errorf("step[0] owner = %q, want trash", steps[0].Owner.Kind)
	}
	if steps[0].Owner.OriginalEpisode != e1 {
		t.Errorf("step[0] originalEpisode = %s, want %s", steps[0].Owner.OriginalEpisode, e1)
	}
	if steps[0].Owner.Record == nil {
		t.Fatal("step[0] Record nil")
	}
	if steps[1].Owner.Kind != OwnerEpisode || steps[1].Owner.EpisodeIntent != "replace" {
		t.Errorf("step[1] owner = %+v, want episode/replace", steps[1].Owner)
	}
}

func TestPlanEpisodes_ActiveOnlyCanonicalMove(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Show")
	e1 := mustEpisode(t, 1, 1)
	// Path is non-canonical (lowercase, missing source token); planner
	// should emit a move to the canonical name.
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			e1: {Active: &media.Record{
				Path:   "/lib/Show/Season 1/raw.mkv",
				Source: media.ParseSource("BluRay"),
			}},
		},
	}
	steps, err := planEpisodes(testToken, seriesRef, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if steps[0].Owner.EpisodeIntent != "move" {
		t.Errorf("intent = %q, want move", steps[0].Owner.EpisodeIntent)
	}
	if filepath.IsAbs(steps[0].From) {
		t.Errorf("From should be series-relative: %q", steps[0].From)
	}
}

func TestPlanEpisodes_StagedSamePathSelfRefreshSkipsTrash(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Show")
	e1 := mustEpisode(t, 1, 1)
	// Self-refresh: Staged points at the same physical file as Active.
	// Planner should NOT emit a trash step (no displacement).
	path := "/lib/Show/Season 1/keep.mkv"
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			e1: {
				Active: &media.Record{Path: path, Source: media.ParseSource("BluRay")},
				Staged: &media.Record{Path: path, Source: media.ParseSource("BluRay")},
			},
		},
	}
	steps, err := planEpisodes(testToken, seriesRef, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.Owner.Kind == OwnerTrash {
			t.Fatalf("self-refresh emitted trash step: %+v", s)
		}
	}
	// Should still emit a primary stage step with intent="add".
	if len(steps) != 1 || steps[0].Owner.EpisodeIntent != "add" {
		t.Fatalf("steps = %+v, want 1 episode/add step", steps)
	}
}

// In-place metadata override: Staged.Path is a series: selector
// pointing at the same physical file as the absolute Active.Path. The
// planner must treat them as the same file (skip trash) even though
// the strings differ in scheme + form (relative vs absolute).
func TestPlanEpisodes_StagedSeriesSelectorEqualsActive(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Show")
	e1 := mustEpisode(t, 1, 1)
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			e1: {
				Active: &media.Record{Path: "/lib/Show/Season 1/keep.mkv", Source: media.ParseSource("Unknown")},
				Staged: &media.Record{Path: "series:Season 1/keep.mkv", Source: media.ParseSource("BluRay")},
			},
		},
	}
	steps, err := planEpisodes(testToken, seriesRef, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if s.Owner.Kind == OwnerTrash {
			t.Fatalf("in-place override emitted trash step: %+v", s)
		}
	}
	if len(steps) != 1 || steps[0].Owner.EpisodeIntent != "add" {
		t.Fatalf("steps = %+v, want 1 episode/add step", steps)
	}
}

func TestPlanEpisodes_NFDActivePathTreatedAsCanonical(t *testing.T) {
	// Filesystem stored basename in NFD form (SMB/AFP NAS, legacy
	// HFS+). The series ref + episode marker + facts compose a
	// canonical NFC destination. Without NFC-equivalent comparison
	// the planner emits a no-op normalization move that the next
	// scan undoes — the operator gets stuck in a loop. Regression
	// guard.
	seriesRef, _ := refs.ParseSeries("ぐらんぶる") // ぐらんぶる NFC
	e1 := mustEpisode(t, 1, 1)
	resolution, _ := media.ParseResolution("1280x720")
	// NFD form of the same basename (く + combining dakuten, ふ +
	// combining dakuten).
	nfdRel := "Season 1/ぐらんぶる - S01E01 (Unknown 720p).mp4"
	if nfdRel == textnorm.NFC(nfdRel).String() {
		t.Fatal("test setup: nfdRel literal is NFC; the regression guard cannot exercise NFD")
	}
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			e1: {Active: &media.Record{
				Path:       nfdRel,
				Source:     media.SourceUnknown,
				Resolution: resolution,
			}},
		},
	}
	steps, err := planEpisodes(testToken, seriesRef, "/lib/ぐらんぶる", model)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %d (%+v), want 0 — NFD basename should be treated as canonical", len(steps), steps)
	}
}

func TestPathsEquivalentNFC(t *testing.T) {
	// NFC + NFD form of the same string should be equivalent.
	nfc := "ぐ"  // ぐ
	nfd := "ぐ" // く + combining dakuten
	if !pathsEquivalentNFC(nfc, nfd) {
		t.Errorf("NFC vs NFD not equivalent")
	}
	if !pathsEquivalentNFC("a", "a") {
		t.Errorf("identical strings not equivalent")
	}
	if pathsEquivalentNFC("a", "b") {
		t.Errorf("different strings reported equivalent")
	}
}

func TestPlanEpisodes_DeterministicAcrossCalls(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Show")
	e1 := mustEpisode(t, 1, 1)
	e2 := mustEpisode(t, 1, 2)
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			e1: {Staged: &media.Record{Path: "/inbox/a.mkv"}},
			e2: {Staged: &media.Record{Path: "/inbox/b.mkv"}},
		},
	}
	a, _ := planEpisodes(testToken, seriesRef, "/lib/Show", model)
	b, _ := planEpisodes(testToken, seriesRef, "/lib/Show", model)
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("step[%d] ID mismatch: %s vs %s", i, a[i].ID, b[i].ID)
		}
	}
}
