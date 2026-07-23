package workflow

import (
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/reconcile"
)

func TestPlanToResponseProjectsStagedRecord(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Bookworm")
	ep, _ := refs.NewEpisode(1, 1)
	mtime := time.Date(2026, 4, 15, 9, 30, 0, 0, time.UTC)
	staged := &reconcile.ReplacedRecord{
		Source: "bluray", Resolution: "3840x2160", Codec: "hevc", Size: 8_000_000_000, MTime: mtime,
	}
	plan := reconcile.Plan{
		Header: reconcile.Header{Series: seriesRef},
		Steps: []reconcile.Step{{
			ID:    "S1",
			Kind:  reconcile.StepFileMove,
			Owner: reconcile.Owner{Kind: reconcile.OwnerEpisode, EpisodeRef: ep, EpisodeIntent: "add", StagedRecord: staged},
			From:  "inbox:in.mkv",
			To:    "Season 1/Bookworm - S01E01 (BluRay 4K).mkv",
		}},
	}
	out := planToResponse(plan)
	if len(out.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(out.Changes))
	}
	c := out.Changes[0]
	if c.Source != "bluray" || c.Resolution != "3840x2160" || c.Codec != "hevc" || c.Size != 8_000_000_000 {
		t.Fatalf("change facts mismatch: %+v", c)
	}
	if c.MTime == nil || !c.MTime.Equal(mtime) {
		t.Fatalf("MTime mismatch: %v", c.MTime)
	}
}

func TestPlanToResponseRoutesReplacedActiveToReplaced(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Bookworm")
	ep, _ := refs.NewEpisode(1, 2)
	mtime := time.Date(2026, 4, 15, 9, 30, 0, 0, time.UTC)
	staged := &reconcile.ReplacedRecord{Source: "bluray", Resolution: "3840x2160", Codec: "hevc", Size: 9e9, MTime: mtime}
	displaced := &reconcile.ReplacedRecord{Source: "webrip", Resolution: "1920x1080", Codec: "h264", Size: 2e9, MTime: mtime}

	plan := reconcile.Plan{
		Header: reconcile.Header{Series: seriesRef},
		Steps: []reconcile.Step{
			{
				ID: "T1", Kind: reconcile.StepFileMove,
				Owner: reconcile.Owner{Kind: reconcile.OwnerTrash, TrashID: "TID", OriginalEpisode: ep, Record: displaced},
				From:  "Season 1/old.mkv", To: ".kura/trash/TID/old.mkv",
			},
			{
				ID: "T2", Kind: reconcile.StepFileMove,
				Owner: reconcile.Owner{Kind: reconcile.OwnerTrash, TrashID: "TID", OriginalEpisode: ep, Record: displaced},
				From:  "Season 1/old.en.srt", To: ".kura/trash/TID/old.en.srt",
			},
			{
				ID: "E1", Kind: reconcile.StepFileMove,
				Owner: reconcile.Owner{Kind: reconcile.OwnerEpisode, EpisodeRef: ep, EpisodeIntent: "replace", StagedRecord: staged},
				From:  "inbox:new.mkv", To: "Season 1/new.mkv",
			},
			{
				ID: "E2", Kind: reconcile.StepFileMove,
				Owner: reconcile.Owner{Kind: reconcile.OwnerEpisode, EpisodeRef: ep, EpisodeIntent: "replace", StagedRecord: staged},
				From:  "inbox:new.en.srt", To: "Season 1/new.en.srt",
			},
		},
	}
	out := planToResponse(plan)
	if len(out.TrashItems) != 0 {
		t.Fatalf("TrashItems = %d, want 0 (replaced-active belongs in Replaced)", len(out.TrashItems))
	}
	if len(out.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(out.Changes))
	}
	c := out.Changes[0]
	if c.Replaced == nil {
		t.Fatal("Replaced nil; expected populated from trash steps")
	}
	if c.Replaced.Source != "webrip" || c.Replaced.Resolution != "1920x1080" || c.Replaced.Codec != "h264" {
		t.Fatalf("Replaced facts mismatch: %+v", c.Replaced)
	}
	if len(c.Replaced.Companions) != 1 || c.Replaced.Companions[0].From != "Season 1/old.en.srt" {
		t.Fatalf("Replaced companions mismatch: %+v", c.Replaced.Companions)
	}
	if len(c.Replaced.StepIDs) != 2 {
		t.Fatalf("Replaced.StepIDs = %v, want both trash step IDs", c.Replaced.StepIDs)
	}
	if len(c.Companions) != 1 || c.Companions[0].From != "inbox:new.en.srt" {
		t.Fatalf("change companions mismatch: %+v", c.Companions)
	}
	if len(c.StepIDs) != 2 {
		t.Fatalf("change StepIDs = %v, want both episode step IDs", c.StepIDs)
	}
}

func TestPlanToResponseStandaloneTrashKeepsFacts(t *testing.T) {
	seriesRef, _ := refs.ParseSeries("Bookworm")
	mtime := time.Date(2026, 4, 15, 9, 30, 0, 0, time.UTC)
	rec := &reconcile.ReplacedRecord{Path: "Season 1/junk.mkv", Size: 5e8, MTime: mtime}
	plan := reconcile.Plan{
		Header: reconcile.Header{Series: seriesRef},
		Steps: []reconcile.Step{{
			ID: "T1", Kind: reconcile.StepFileMove,
			Owner: reconcile.Owner{Kind: reconcile.OwnerTrash, TrashID: "TID", Record: rec},
			From:  "Season 1/junk.mkv", To: ".kura/trash/TID/junk.mkv",
		}},
	}
	out := planToResponse(plan)
	if len(out.TrashItems) != 1 {
		t.Fatalf("TrashItems = %d, want 1", len(out.TrashItems))
	}
	ti := out.TrashItems[0]
	if ti.Size != 5e8 || ti.MTime == nil || !ti.MTime.Equal(mtime) {
		t.Fatalf("trash facts mismatch: %+v", ti)
	}
	if ti.Source != "" || ti.Resolution != "" || ti.Codec != "" {
		t.Fatalf("standalone trash should have empty media facts: %+v", ti)
	}
}
