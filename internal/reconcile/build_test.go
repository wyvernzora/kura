package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// TestBuildPlan_StandaloneTrashPrecedesEpisodeSteps pins down the global
// step ordering: a standalone stagedTrash entry must produce a step that
// runs before any episode (or extras) step. Otherwise an episode
// canonicalization landing at the same path as a staged-trash file would
// clobber the file the user told Kura to preserve in trash.
func TestBuildPlan_StandaloneTrashPrecedesEpisodeSteps(t *testing.T) {
	libRoot := t.TempDir()
	ref, err := refs.ParseSeries("Show")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	e1, err := refs.NewEpisode(1, 1)
	if err != nil {
		t.Fatalf("NewEpisode: %v", err)
	}

	trashID := ulid.Make()
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{
			// Non-canonical active record forces a canonicalization move
			// from planEpisodes.
			e1: {Active: &media.Record{
				Path:   "Season 1/raw.mkv",
				Source: media.ParseSource("BluRay"),
			}},
		},
		StagedTrash: []series.StagedTrashItem{{
			ID:      trashID,
			Path:    "Season 1/loser.mkv",
			Size:    100,
			MTime:   time.Unix(0, 0),
			AddedAt: time.Unix(0, 0),
		}},
	}
	if err := seriesfile.SaveCAS(libRoot, model, coord.Mutator{Op: "test_init", PID: 1, Host: "h", At: time.Unix(0, 0)}); err != nil {
		t.Fatalf("SaveCAS: %v", err)
	}

	deps := Deps{LibRoot: libRoot, Now: func() time.Time { return time.Unix(0, 0) }}
	plan, err := BuildPlan(context.Background(), deps, PlanInput{Ref: ref})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	firstTrash := -1
	firstEpisode := -1
	for i, s := range plan.Steps {
		switch s.Owner.Kind {
		case OwnerTrash:
			// Only count standalone trash (no OriginalEpisode). Episode-
			// internal "trash the displaced active" steps are owned by
			// trash but carry an OriginalEpisode.
			if !s.Owner.OriginalEpisode.IsZero() {
				continue
			}
			if firstTrash < 0 {
				firstTrash = i
			}
		case OwnerEpisode:
			if firstEpisode < 0 {
				firstEpisode = i
			}
		}
	}
	if firstTrash < 0 {
		t.Fatalf("no standalone trash step emitted; plan = %+v", plan.Steps)
	}
	if firstEpisode < 0 {
		t.Fatalf("no episode step emitted; plan = %+v", plan.Steps)
	}
	if firstTrash >= firstEpisode {
		t.Errorf("standalone trash step at index %d must precede first episode step at index %d", firstTrash, firstEpisode)
	}
	if plan.Steps[firstTrash].Owner.TrashID != trashID.String() {
		t.Errorf("trash step TrashID = %q, want %q", plan.Steps[firstTrash].Owner.TrashID, trashID)
	}
}
