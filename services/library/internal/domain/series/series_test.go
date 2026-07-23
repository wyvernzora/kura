package series

import (
	"testing"

	"cloud.google.com/go/civil"
	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/services/library/internal/domain/media"
	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
)

func mustParseDate(t *testing.T, value string) civil.Date {
	t.Helper()
	d, err := civil.ParseDate(value)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", value, err)
	}
	return d
}

func TestPruneSpineRemovesEmptyOrphans(t *testing.T) {
	known, _ := refs.NewEpisode(1, 1)
	orphan, _ := refs.NewEpisode(1, 2)
	model := Series{
		Episodes: map[refs.Episode]Episode{
			known:  {},
			orphan: {},
		},
	}
	conflicts := model.PruneSpine(map[refs.Episode]struct{}{known: {}})
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %v, want none", conflicts)
	}
	if _, ok := model.Episodes[orphan]; ok {
		t.Fatal("PruneSpine left empty orphan slot in place")
	}
	if _, ok := model.Episodes[known]; !ok {
		t.Fatal("PruneSpine removed known slot")
	}
}

func TestPruneSpineKeepsOrphansWithRecords(t *testing.T) {
	known, _ := refs.NewEpisode(1, 1)
	stagedOrphan, _ := refs.NewEpisode(1, 2)
	activeOrphan, _ := refs.NewEpisode(1, 3)
	model := Series{
		Episodes: map[refs.Episode]Episode{
			known:        {},
			stagedOrphan: {Staged: &media.Record{Path: "/inbox/x.mkv"}},
			activeOrphan: {Active: &media.Record{Path: "Season 1/y.mkv"}},
		},
	}
	conflicts := model.PruneSpine(map[refs.Episode]struct{}{known: {}})
	if len(conflicts) != 2 {
		t.Fatalf("len(conflicts) = %d, want 2", len(conflicts))
	}
	for _, ref := range []refs.Episode{stagedOrphan, activeOrphan} {
		if _, ok := model.Episodes[ref]; !ok {
			t.Fatalf("PruneSpine removed orphan %s with active/staged record", ref)
		}
	}
}

func TestRefreshSpineNeverRemovesEpisodes(t *testing.T) {
	oldRef, _ := refs.NewEpisode(1, 1)
	newRef, _ := refs.NewEpisode(1, 2)
	model := Series{
		Metadata: refs.Metadata("tvdb:370070"),
		Episodes: map[refs.Episode]Episode{
			oldRef: {AirDate: mustParseDate(t, "2019-10-02")},
		},
	}
	model.RefreshSpine([]SpineEntry{{Ref: newRef, AirDate: mustParseDate(t, "2019-10-09")}})
	if _, ok := model.Episodes[oldRef]; !ok {
		t.Fatal("RefreshSpine removed old spine entry")
	}
	if got := model.Episodes[newRef].AirDate.String(); got != "2019-10-09" {
		t.Fatalf("new air date = %q", got)
	}
}

func TestStagedTrashHelpers(t *testing.T) {
	model := Series{}
	id1 := ulid.Make()
	id2 := ulid.Make()

	model.AddStagedTrash(StagedTrashItem{ID: id1, Path: "a.mkv"})
	model.AddStagedTrash(StagedTrashItem{ID: id2, Path: "b.mkv"})
	if len(model.StagedTrash) != 2 {
		t.Fatalf("after Add x2: len = %d, want 2", len(model.StagedTrash))
	}

	if !model.RemoveStagedTrash(id1) {
		t.Fatal("RemoveStagedTrash(id1) = false, want true")
	}
	if len(model.StagedTrash) != 1 || model.StagedTrash[0].ID != id2 {
		t.Fatalf("after Remove(id1): %+v", model.StagedTrash)
	}

	if model.RemoveStagedTrash(ulid.Make()) {
		t.Fatal("RemoveStagedTrash(unknown) = true, want false (no-op)")
	}

	model.ClearStagedTrash()
	if len(model.StagedTrash) != 0 {
		t.Fatalf("after Clear: len = %d, want 0", len(model.StagedTrash))
	}
}

func TestStagedExtrasHelpers(t *testing.T) {
	model := Series{}
	id1 := ulid.Make()
	id2 := ulid.Make()

	model.AddStagedExtra(StagedExtraItem{ID: id1, Season: 1, Path: "/x/foo"})
	model.AddStagedExtra(StagedExtraItem{ID: id2, Season: 2, Path: "/x/bar"})
	if len(model.StagedExtras) != 2 {
		t.Fatalf("after Add x2: len = %d, want 2", len(model.StagedExtras))
	}

	if !model.RemoveStagedExtra(id1) {
		t.Fatal("RemoveStagedExtra(id1) = false, want true")
	}
	if len(model.StagedExtras) != 1 || model.StagedExtras[0].ID != id2 {
		t.Fatalf("after Remove(id1): %+v", model.StagedExtras)
	}

	model.ClearStagedExtras()
	if len(model.StagedExtras) != 0 {
		t.Fatalf("after Clear: len = %d, want 0", len(model.StagedExtras))
	}
}

// Silence unused-import warning if no other test references media here.
var _ = media.Companion{}
