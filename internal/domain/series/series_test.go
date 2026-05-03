package series

import (
	"testing"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
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
