package planfile_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/reconcile"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
	"github.com/wyvernzora/kura/services/library/internal/storage/planfile"
)

const (
	tokenA = "0123456789ab"
	tokenB = "fedcba987654"
)

func samplePlan(t *testing.T, token string) reconcile.Plan {
	t.Helper()
	seriesRef, err := refs.ParseSeries("Bookworm")
	if err != nil {
		t.Fatal(err)
	}
	episode, _ := refs.NewEpisode(1, 1)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	owner := reconcile.Owner{Kind: reconcile.OwnerEpisode, EpisodeRef: episode}
	step := reconcile.Step{
		ID:    "AAAAAAAAAAAAAAAA",
		Kind:  reconcile.StepFileMove,
		Owner: owner,
		From:  "/inbox/Bookworm S01E01.mkv",
		To:    "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
	}
	return reconcile.Plan{
		Header: reconcile.Header{
			SchemaVersion: 2,
			Token:         token,
			CreatedAt:     now,
			Series:        seriesRef,
			Snapshot:      "abc123",
		},
		Steps: []reconcile.Step{step},
	}
}

func TestWriteReadPlanRoundTrip(t *testing.T) {
	root := t.TempDir()
	in := samplePlan(t, tokenA)
	if err := planfile.WritePlan(root, in.Header.Series, in); err != nil {
		t.Fatal(err)
	}
	out, applied, err := planfile.ReadPlan(root, in.Header.Series, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("ReadPlan applied = true on fresh file")
	}
	if out.Header.Token != in.Header.Token || out.Header.Series != in.Header.Series || out.Header.Snapshot != in.Header.Snapshot {
		t.Fatalf("header mismatch: %#v", out.Header)
	}
	if len(out.Steps) != 1 || out.Steps[0].To != in.Steps[0].To {
		t.Fatalf("steps mismatch: %#v", out.Steps)
	}
}

func TestWriteReadPlanPreservesOwnerRecords(t *testing.T) {
	root := t.TempDir()
	seriesRef, err := refs.ParseSeries("Bookworm")
	if err != nil {
		t.Fatal(err)
	}
	episode, _ := refs.NewEpisode(1, 1)
	original, _ := refs.NewEpisode(1, 1)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mtime := time.Date(2026, 4, 15, 9, 30, 0, 0, time.UTC)

	stagedRec := &reconcile.ReplacedRecord{
		Path:       "inbox:/incoming/Bookworm S01E01 2160p BluRay HEVC.mkv",
		Source:     "bluray",
		Resolution: "3840x2160",
		Codec:      "hevc",
		Size:       8_000_000_000,
		MTime:      mtime,
		Companions: []reconcile.ReplacedCompanion{{
			Path: "inbox:/incoming/Bookworm S01E01.en.srt", Role: "subtitle", Language: "en", Size: 12_345, MTime: mtime,
		}},
		Attrs: map[string]string{"origin": "takuhai"},
	}
	displacedRec := &reconcile.ReplacedRecord{
		Path:       "Season 1/Bookworm - S01E01 (WebRip 1080p).mkv",
		Source:     "webrip",
		Resolution: "1920x1080",
		Codec:      "h264",
		Size:       2_500_000_000,
		MTime:      mtime,
		Attrs:      map[string]string{"release_group": "OldGroup"},
	}

	episodeStep := reconcile.Step{
		ID:   "AAAAAAAAAAAAAAAA",
		Kind: reconcile.StepFileMove,
		Owner: reconcile.Owner{
			Kind:          reconcile.OwnerEpisode,
			EpisodeRef:    episode,
			EpisodeIntent: "replace",
			StagedRecord:  stagedRec,
		},
		From: stagedRec.Path,
		To:   "Season 1/Bookworm - S01E01 (BluRay 4K).mkv",
	}
	trashStep := reconcile.Step{
		ID:   "BBBBBBBBBBBBBBBB",
		Kind: reconcile.StepFileMove,
		Owner: reconcile.Owner{
			Kind:            reconcile.OwnerTrash,
			TrashID:         "01J0000000000000000000000T",
			OriginalEpisode: original,
			Record:          displacedRec,
		},
		From: displacedRec.Path,
		To:   ".kura/trash/01J0000000000000000000000T/Bookworm - S01E01 (WebRip 1080p).mkv",
	}

	in := reconcile.Plan{
		Header: reconcile.Header{
			SchemaVersion: 2,
			Token:         tokenA,
			CreatedAt:     now,
			Series:        seriesRef,
			Snapshot:      "abc123",
		},
		Steps: []reconcile.Step{trashStep, episodeStep},
	}
	if err := planfile.WritePlan(root, seriesRef, in); err != nil {
		t.Fatal(err)
	}
	out, _, err := planfile.ReadPlan(root, seriesRef, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 2 {
		t.Fatalf("steps len = %d, want 2", len(out.Steps))
	}
	gotEp := out.Steps[1].Owner.StagedRecord
	if gotEp == nil {
		t.Fatal("episode step StagedRecord nil after round-trip")
	}
	if gotEp.Source != "bluray" || gotEp.Resolution != "3840x2160" || gotEp.Codec != "hevc" || gotEp.Size != 8_000_000_000 || !gotEp.MTime.Equal(mtime) {
		t.Fatalf("StagedRecord facts mismatch: %#v", gotEp)
	}
	if len(gotEp.Companions) != 1 || gotEp.Companions[0].Role != "subtitle" || gotEp.Companions[0].Language != "en" {
		t.Fatalf("StagedRecord companions mismatch: %#v", gotEp.Companions)
	}
	if gotEp.Attrs["origin"] != "takuhai" {
		t.Fatalf("StagedRecord attrs = %#v", gotEp.Attrs)
	}
	gotTrash := out.Steps[0].Owner.Record
	if gotTrash == nil {
		t.Fatal("trash step Record nil after round-trip")
	}
	if gotTrash.Source != "webrip" || gotTrash.Codec != "h264" || gotTrash.Size != 2_500_000_000 {
		t.Fatalf("Record facts mismatch: %#v", gotTrash)
	}
	if gotTrash.Attrs["release_group"] != "OldGroup" {
		t.Fatalf("Record attrs = %#v", gotTrash.Attrs)
	}
	if out.Steps[1].Owner.EpisodeIntent != "replace" {
		t.Fatalf("EpisodeIntent = %q, want replace", out.Steps[1].Owner.EpisodeIntent)
	}
	if out.Steps[0].Owner.OriginalEpisode != original {
		t.Fatalf("OriginalEpisode = %v, want %v", out.Steps[0].Owner.OriginalEpisode, original)
	}
}

func TestAppendLogMarksApplied(t *testing.T) {
	root := t.TempDir()
	in := samplePlan(t, tokenA)
	if err := planfile.WritePlan(root, in.Header.Series, in); err != nil {
		t.Fatal(err)
	}
	log, err := planfile.OpenLog(root, in.Header.Series, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 1, 0, 0, time.UTC)
	if err := log.AppendEvent(now, in.Steps[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := log.AppendResult(now, "success", 1, nil); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	_, applied, err := planfile.ReadPlan(root, in.Header.Series, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("ReadPlan applied = false after success result")
	}
}

func TestReadPlanRejectsTokenMismatch(t *testing.T) {
	root := t.TempDir()
	in := samplePlan(t, tokenB)
	if err := planfile.WritePlan(root, in.Header.Series, in); err != nil {
		t.Fatal(err)
	}
	srcPath := paths.PlanFile(root, in.Header.Series, tokenB)
	dstPath := paths.PlanFile(root, in.Header.Series, tokenA)
	if err := os.Rename(srcPath, dstPath); err != nil {
		t.Fatal(err)
	}
	_, _, err := planfile.ReadPlan(root, in.Header.Series, tokenA)
	if err == nil || !strings.Contains(err.Error(), "token mismatch") {
		t.Fatalf("ReadPlan err = %v, want token mismatch", err)
	}
}

func TestListTokens(t *testing.T) {
	root := t.TempDir()
	in := samplePlan(t, tokenA)
	if err := planfile.WritePlan(root, in.Header.Series, in); err != nil {
		t.Fatal(err)
	}
	tokens, err := planfile.ListTokens(root, in.Header.Series)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != tokenA {
		t.Fatalf("ListTokens = %v", tokens)
	}
}

func TestReadPlanRejectsV1(t *testing.T) {
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Bookworm")
	if err := os.MkdirAll(paths.PlanDir(root, seriesRef), 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a v1-shaped first line (schemaVersion=1).
	v1 := `{"type":"plan","schemaVersion":1,"token":"0123456789ab","createdAt":"2026-01-01T00:00:00Z","expiresAt":"2026-01-01T00:05:00Z","plan":{"series":"Bookworm","snapshot":"abc","changes":[]}}` + "\n"
	if err := os.WriteFile(paths.PlanFile(root, seriesRef, tokenA), []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := planfile.ReadPlan(root, seriesRef, tokenA)
	// Either "type plan want header" (line-1 type discriminator
	// catches v1 first) or "schemaVersion 1" — either is a clear
	// rejection that tells the caller to re-plan.
	if err == nil {
		t.Fatal("ReadPlan err = nil, want v1 rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, "type") && !strings.Contains(msg, "schemaVersion") {
		t.Fatalf("ReadPlan err = %v, want v1 rejection", err)
	}
}

func TestReadPlanToleratesTornTrailingLine(t *testing.T) {
	root := t.TempDir()
	in := samplePlan(t, tokenA)
	if err := planfile.WritePlan(root, in.Header.Series, in); err != nil {
		t.Fatal(err)
	}
	log, err := planfile.OpenLog(root, in.Header.Series, tokenA)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 1, 12, 1, 0, 0, time.UTC)
	if err := log.AppendEvent(now, in.Steps[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	// Corrupt the last byte to simulate a torn write mid-line.
	planPath := paths.PlanFile(root, in.Header.Series, tokenA)
	fi, err := os.Stat(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(planPath, fi.Size()-1); err != nil {
		t.Fatal(err)
	}

	_, applied, err := planfile.ReadPlan(root, in.Header.Series, tokenA)
	if err != nil {
		t.Fatalf("ReadPlan: unexpected error on torn trailing line: %v", err)
	}
	if applied {
		t.Fatal("ReadPlan: applied = true despite no result line")
	}
}

func TestWritePlanRejectsBadToken(t *testing.T) {
	root := t.TempDir()
	seriesRef, _ := refs.ParseSeries("Bookworm")
	cases := []struct {
		name  string
		token string
	}{
		{"too short", "abc"},
		{"too long", "0123456789abcdef"},
		{"non-hex", "ZZZZZZZZZZZZ"},
		{"uppercase", "0123456789AB"},
		{"ulid", "01KQN3TH7H75ATNP4DV1YQ6487"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := reconcile.Plan{Header: reconcile.Header{
				SchemaVersion: 2,
				Token:         tc.token,
				CreatedAt:     time.Now().UTC(),
				Series:        seriesRef,
			}}
			err := planfile.WritePlan(root, seriesRef, plan)
			if err == nil || !errors.Is(err, err) || !strings.Contains(err.Error(), "invalid token") {
				t.Fatalf("WritePlan err = %v, want invalid token", err)
			}
		})
	}
}
