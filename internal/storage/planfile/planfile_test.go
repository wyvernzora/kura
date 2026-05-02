package planfile_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/domain/reconcile"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/planfile"
)

func TestWriteReadPlanRoundTrip(t *testing.T) {
	root := t.TempDir()
	seriesRef, err := refs.ParseSeries("Bookworm")
	if err != nil {
		t.Fatal(err)
	}
	episode, _ := refs.NewEpisode(1, 1)
	token := ulid.Make().String()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	in := planfile.PlanRecord{
		Token:     token,
		CreatedAt: now,
		ExpiresAt: now.Add(5 * time.Minute),
		Plan: reconcile.Plan{
			Series:   seriesRef,
			Snapshot: "abc123",
			Changes: []reconcile.Change{
				{
					Kind:       reconcile.ChangeAdd,
					Episode:    episode,
					FileMove:   reconcile.FileMove{From: "/inbox/Bookworm S01E01.mkv", To: "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv"},
					Source:     "webrip",
					Resolution: "1920x1080",
					Companions: []reconcile.FileMove{
						{From: "/inbox/Bookworm S01E01.en.srt", To: "Season 1/Bookworm - S01E01 (WebRip 1080p).en.srt"},
					},
				},
			},
		},
	}
	if err := planfile.WritePlan(root, seriesRef, in); err != nil {
		t.Fatal(err)
	}
	out, applied, err := planfile.ReadPlan(root, seriesRef, token)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("ReadPlan applied = true on fresh file")
	}
	if out.Token != in.Token || out.Plan.Series != in.Plan.Series || out.Plan.Snapshot != in.Plan.Snapshot {
		t.Fatalf("round-trip mismatch: %#v", out)
	}
	if len(out.Plan.Changes) != 1 || out.Plan.Changes[0].To != in.Plan.Changes[0].To {
		t.Fatalf("changes mismatch: %#v", out.Plan.Changes)
	}
	if len(out.Plan.Changes[0].Companions) != 1 {
		t.Fatalf("companion count = %d", len(out.Plan.Changes[0].Companions))
	}
}

func TestAppendLogMarksApplied(t *testing.T) {
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Bookworm")
	token := ulid.Make().String()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	plan := planfile.PlanRecord{
		Token:     token,
		CreatedAt: now,
		ExpiresAt: now.Add(5 * time.Minute),
		Plan:      reconcile.Plan{Series: seriesRef},
	}
	if err := planfile.WritePlan(root, seriesRef, plan); err != nil {
		t.Fatal(err)
	}
	log, err := planfile.OpenLog(root, seriesRef, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.AppendMove(now, 1, 1, reconcile.FileMove{From: "a", To: "b"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := log.AppendResult(now, "success", 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	_, applied, err := planfile.ReadPlan(root, seriesRef, token)
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("ReadPlan applied = false after success result")
	}
}

func TestReadPlanRejectsTokenMismatch(t *testing.T) {
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Bookworm")
	token := ulid.Make().String()
	other := ulid.Make().String()
	plan := planfile.PlanRecord{
		Token:     other,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		Plan:      reconcile.Plan{Series: seriesRef},
	}
	if err := planfile.WritePlan(root, seriesRef, plan); err != nil {
		t.Fatal(err)
	}
	srcPath := paths.PlanFile(root, seriesRef, other)
	dstPath := paths.PlanFile(root, seriesRef, token)
	if err := os.Rename(srcPath, dstPath); err != nil {
		t.Fatal(err)
	}
	_, _, err := planfile.ReadPlan(root, seriesRef, token)
	if err == nil || !strings.Contains(err.Error(), "token mismatch") {
		t.Fatalf("ReadPlan err = %v, want token mismatch", err)
	}
}

func TestListTokens(t *testing.T) {
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Bookworm")
	token := ulid.Make().String()
	plan := planfile.PlanRecord{
		Token:     token,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		Plan:      reconcile.Plan{Series: seriesRef},
	}
	if err := planfile.WritePlan(root, seriesRef, plan); err != nil {
		t.Fatal(err)
	}
	tokens, err := planfile.ListTokens(root, seriesRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != token {
		t.Fatalf("ListTokens = %v", tokens)
	}
}

func TestWritePlanRejectsBadToken(t *testing.T) {
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Bookworm")
	plan := planfile.PlanRecord{
		Token:     "not-a-ulid",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Minute),
		Plan:      reconcile.Plan{Series: seriesRef},
	}
	err := planfile.WritePlan(root, seriesRef, plan)
	if err == nil || !errors.Is(err, err) || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("WritePlan err = %v, want invalid token", err)
	}
}
