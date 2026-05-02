package reconcile

import (
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/series"
)

func TestPlanTrash_EmptyArrayProducesNoSteps(t *testing.T) {
	model := &series.Series{}
	steps, err := planTrash(testToken, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %d, want 0", len(steps))
	}
}

func TestPlanTrash_EmitsFileMovePerEntry(t *testing.T) {
	id1 := ulid.Make()
	model := &series.Series{
		StagedTrash: []series.StagedTrashItem{{
			ID:    id1,
			Path:  "/lib/Show/Season 1/loser.mkv",
			Size:  100,
			MTime: time.Now(),
		}},
	}
	steps, err := planTrash(testToken, "/lib/Show", model)
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
	if step.Owner.Kind != OwnerTrash {
		t.Errorf("owner = %q, want trash", step.Owner.Kind)
	}
	if step.Owner.TrashID != id1.String() {
		t.Errorf("TrashID = %q", step.Owner.TrashID)
	}
	if !step.Owner.OriginalEpisode.IsZero() {
		t.Errorf("standalone trash should have empty OriginalEpisode, got %s", step.Owner.OriginalEpisode)
	}
	if step.Owner.Record == nil {
		t.Fatal("Record nil — apply needs it for trashfile.Meta")
	}
	if step.Owner.Record.Path != "Season 1/loser.mkv" {
		t.Errorf("Record.Path = %q, want series-relative slash form", step.Owner.Record.Path)
	}
	if step.From != "series:Season 1/loser.mkv" {
		t.Errorf("From = %q", step.From)
	}
	if !strings.Contains(step.To, ".kura/trash/"+id1.String()+"/") {
		t.Errorf("To = %q, want trash bucket path (got bare or series:-prefixed)", step.To)
	}
}

func TestPlanTrash_CompanionsEmitOwnSteps(t *testing.T) {
	id := ulid.Make()
	model := &series.Series{
		StagedTrash: []series.StagedTrashItem{{
			ID:   id,
			Path: "/lib/Show/Season 1/loser.mkv",
			Companions: []media.Companion{
				{Path: "/lib/Show/Season 1/loser.srt"},
			},
		}},
	}
	steps, err := planTrash(testToken, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2 (media + companion)", len(steps))
	}
	if steps[0].Owner.TrashID != steps[1].Owner.TrashID {
		t.Error("media and companion don't share TrashID")
	}
	if !strings.HasSuffix(steps[1].To, "loser.srt") {
		t.Errorf("companion To = %q, want .srt suffix", steps[1].To)
	}
}

func TestPlanTrash_DeterministicSortByULID(t *testing.T) {
	// Two items added in non-sorted order; output should sort by ULID.
	id1 := ulid.MustParse("01H0000000000000000000AAAA")
	id2 := ulid.MustParse("01H0000000000000000000BBBB")
	model := &series.Series{
		StagedTrash: []series.StagedTrashItem{
			{ID: id2, Path: "/lib/Show/Season 1/b.mkv"},
			{ID: id1, Path: "/lib/Show/Season 1/a.mkv"},
		},
	}
	steps, err := planTrash(testToken, "/lib/Show", model)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Owner.TrashID != id1.String() {
		t.Errorf("first step TrashID = %s, want %s (sorted)", steps[0].Owner.TrashID, id1)
	}
}
