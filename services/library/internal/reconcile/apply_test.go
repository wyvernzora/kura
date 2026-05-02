package reconcile

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/internal/storage/trashfile"
)

type stubLog struct {
	events []struct {
		stepID string
		err    error
	}
}

func (s *stubLog) AppendEvent(_ time.Time, stepID string, err error) error {
	s.events = append(s.events, struct {
		stepID string
		err    error
	}{stepID, err})
	return nil
}

func (s *stubLog) AppendResult(time.Time, string, int, error) error { return nil }

func TestRunSteps_ReportsAppliedAndFailed(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "src1.mkv")
	src3 := filepath.Join(dir, "src3.mkv")
	for _, p := range []string{src1, src3} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	steps := []Step{
		{ID: "AAAA", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: src1, To: filepath.Join(dir, "dst1.mkv")},
		// Step 2 has a missing source — SafeMoveFile errors at stat.
		{ID: "BBBB", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "missing.mkv"), To: filepath.Join(dir, "dst2.mkv")},
		// Step 3 should NOT run after step 2 fails.
		{ID: "CCCC", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: src3, To: filepath.Join(dir, "dst3.mkv")},
	}
	log := &stubLog{}
	exec := &executor{
		deps:      Deps{Now: func() time.Time { return time.Unix(0, 0) }},
		in:        ApplyInput{Plan: Plan{Steps: steps}, Log: log},
		seriesDir: dir,
	}
	applied, failed, err := exec.runSteps(context.Background())
	if err == nil {
		t.Fatal("runSteps: nil err, want failure on step 2")
	}
	var stepErr *ApplyStepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("err %T, want *ApplyStepError", err)
	}
	if stepErr.StepID != "BBBB" {
		t.Errorf("StepID = %s, want BBBB", stepErr.StepID)
	}
	if len(applied) != 1 || applied[0] != "AAAA" {
		t.Errorf("applied = %v, want [AAAA]", applied)
	}
	if failed == nil || failed.ID != "BBBB" {
		t.Errorf("failed = %+v, want step BBBB", failed)
	}
	if len(log.events) != 2 {
		t.Errorf("log events = %d, want 2 (one success + one failure; step CCCC must not run)", len(log.events))
	}
	if _, err := os.Stat(filepath.Join(dir, "dst3.mkv")); !os.IsNotExist(err) {
		t.Errorf("step CCCC should not have run; dst3 stat err=%v", err)
	}
}

func TestRunSteps_CancelCtx_StopsAtStepBoundary(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"src1.mkv", "src2.mkv", "src3.mkv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	steps := []Step{
		{ID: "S1", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "src1.mkv"), To: filepath.Join(dir, "dst1.mkv")},
		{ID: "S2", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "src2.mkv"), To: filepath.Join(dir, "dst2.mkv")},
		{ID: "S3", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "src3.mkv"), To: filepath.Join(dir, "dst3.mkv")},
	}
	log := &stubLog{}
	exec := &executor{
		deps:      Deps{Now: func() time.Time { return time.Unix(0, 0) }},
		in:        ApplyInput{Plan: Plan{Steps: steps}, Log: log},
		seriesDir: dir,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: ctx.Err() fires on first iteration
	applied, failed, err := exec.runSteps(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(applied) != 0 {
		t.Errorf("applied = %v, want none (cancelled before first step)", applied)
	}
	if failed != nil {
		t.Errorf("failed = %+v, want nil", failed)
	}
	for _, name := range []string{"dst1.mkv", "dst2.mkv", "dst3.mkv"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
			t.Errorf("%s should not exist (step must not have run)", name)
		}
	}
}

func TestRunSteps_EmitsProgressEvents(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"src1.mkv", "src2.mkv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	steps := []Step{
		{ID: "S1", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "src1.mkv"), To: filepath.Join(dir, "dst1.mkv")},
		{ID: "S2", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "src2.mkv"), To: filepath.Join(dir, "dst2.mkv")},
	}
	ctx, events := progress.Capture(context.Background())
	exec := &executor{
		deps:      Deps{Now: func() time.Time { return time.Unix(0, 0) }},
		in:        ApplyInput{Plan: Plan{Steps: steps}, Log: &stubLog{}},
		seriesDir: dir,
	}
	if _, _, err := exec.runSteps(ctx); err != nil {
		t.Fatalf("runSteps: %v", err)
	}
	// Expect: Start, Update(1/2), Update(2/2), Success — 4 events.
	if len(*events) != 4 {
		t.Fatalf("progress events = %d, want 4: %+v", len(*events), *events)
	}
	if (*events)[0].Status != progress.StartStatus {
		t.Errorf("events[0].Status = %q, want %q", (*events)[0].Status, progress.StartStatus)
	}
	if (*events)[1].Status != progress.UpdateStatus || (*events)[1].Current != 1 || (*events)[1].Total != 2 {
		t.Errorf("events[1] = %+v, want Update(1/2)", (*events)[1])
	}
	if (*events)[2].Status != progress.UpdateStatus || (*events)[2].Current != 2 || (*events)[2].Total != 2 {
		t.Errorf("events[2] = %+v, want Update(2/2)", (*events)[2])
	}
	if (*events)[3].Status != progress.SuccessStatus {
		t.Errorf("events[3].Status = %q, want %q", (*events)[3].Status, progress.SuccessStatus)
	}
	for _, ev := range *events {
		if ev.Stage != "reconcile_apply" {
			t.Errorf("event.Stage = %q, want %q", ev.Stage, "reconcile_apply")
		}
	}
}

func TestRunSteps_EmitsFailureEvent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "src1.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := []Step{
		{ID: "S1", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "src1.mkv"), To: filepath.Join(dir, "dst1.mkv")},
		{ID: "S2", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "missing.mkv"), To: filepath.Join(dir, "dst2.mkv")},
	}
	ctx, events := progress.Capture(context.Background())
	exec := &executor{
		deps:      Deps{Now: func() time.Time { return time.Unix(0, 0) }},
		in:        ApplyInput{Plan: Plan{Steps: steps}, Log: &stubLog{}},
		seriesDir: dir,
	}
	_, _, err := exec.runSteps(ctx)
	if err == nil {
		t.Fatal("runSteps: nil err, want failure on step 2")
	}
	// Expect: Start, Update(1/2), Update(2/2), Failure — 4 events.
	if len(*events) != 4 {
		t.Fatalf("progress events = %d, want 4: %+v", len(*events), *events)
	}
	if (*events)[0].Status != progress.StartStatus {
		t.Errorf("events[0].Status = %q, want %q", (*events)[0].Status, progress.StartStatus)
	}
	last := (*events)[len(*events)-1]
	if last.Status != progress.FailureStatus {
		t.Errorf("last event Status = %q, want %q", last.Status, progress.FailureStatus)
	}
}

func TestReleaseClaim_DetectsPeerWrite(t *testing.T) {
	libRoot := t.TempDir()
	ref, err := refs.ParseSeries("TestSeries")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}

	// Initial create.
	s := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:1"),
		Episodes: map[refs.Episode]series.Episode{},
	}
	if err := seriesfile.SaveCAS(libRoot, s, coord.Mutator{Op: "test_init", PID: 1, Host: "h", At: time.Now()}); err != nil {
		t.Fatalf("initial SaveCAS: %v", err)
	}

	// Simulate acquireClaim: stamp InProgress + persist.
	holder := coord.NewHolder("reconcile_apply", "tok-1")
	s.InProgress = &holder
	if err := seriesfile.SaveCAS(libRoot, s, coord.Mutator{Op: "test_claim", PID: 1, Host: "h", At: time.Now()}); err != nil {
		t.Fatalf("claim SaveCAS: %v", err)
	}
	claimedHash := s.Hash

	// Peer write: a second writer mutates series.json without touching
	// InProgress. Different mutator op embeds different JSON, so the
	// post-write hash differs from claimedHash.
	peer, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("peer Load: %v", err)
	}
	if err := seriesfile.SaveCAS(libRoot, peer, coord.Mutator{Op: "test_peer_write", PID: 2, Host: "h", At: time.Now()}); err != nil {
		t.Fatalf("peer SaveCAS: %v", err)
	}
	if peer.Hash == claimedHash {
		t.Fatal("peer write produced identical hash; test setup is broken")
	}

	deps := Deps{
		LibRoot: libRoot,
		Now:     func() time.Time { return time.Unix(0, 0) },
	}
	err = releaseClaim(deps, ref, holder, claimedHash)
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("releaseClaim err = %v, want *coord.ConflictError", err)
	}

	// Sanity: with the actual current hash, release succeeds.
	if err := releaseClaim(deps, ref, holder, peer.Hash); err != nil {
		t.Fatalf("releaseClaim with current hash: %v", err)
	}
	cleared, err := seriesfile.Load(libRoot, ref)
	if err != nil {
		t.Fatalf("post-release Load: %v", err)
	}
	if cleared.InProgress != nil {
		t.Fatalf("InProgress = %+v, want nil after release", cleared.InProgress)
	}
}

type failingLog struct {
	appendErr error
}

func (f *failingLog) AppendEvent(time.Time, string, error) error { return nil }
func (f *failingLog) AppendResult(time.Time, string, int, error) error {
	return f.appendErr
}

func TestApply_LogAppendFailureSurfacedViaSlog(t *testing.T) {
	libRoot := t.TempDir()
	ref, err := refs.ParseSeries("LogFailureTest")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}

	var slogBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&slogBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	deps := Deps{
		LibRoot:     libRoot,
		Now:         func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) },
		Coordinator: coord.NewCLICoordinator(),
		Logger:      logger,
	}

	appendSentinel := errors.New("sentinel-append-failure")
	stub := &failingLog{appendErr: appendSentinel}

	// Plan whose Series matches Ref but is already expired → applyLocked
	// hits the PlanExpiredError branch which calls recordFailure.
	plan := Plan{
		Header: Header{
			Token:     "tok-expired",
			Series:    ref,
			ExpiresAt: time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC), // before Now()
		},
		Steps: []Step{
			{ID: "S1", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: "a", To: "b"},
		},
	}

	_, applyErr := Apply(context.Background(), deps, ApplyInput{
		Ref:  ref,
		Plan: plan,
		Log:  stub,
	})
	var expired *PlanExpiredError
	if !errors.As(applyErr, &expired) {
		t.Fatalf("Apply err = %v, want *PlanExpiredError", applyErr)
	}

	got := slogBuf.String()
	if !strings.Contains(got, "apply log append failed") {
		t.Errorf("slog output missing append-failure warning; got:\n%s", got)
	}
	if !strings.Contains(got, "sentinel-append-failure") {
		t.Errorf("slog output missing append-error sentinel; got:\n%s", got)
	}
}

func TestBuildPlan_PrecancelledCtxReturnsEarly(t *testing.T) {
	ref, err := refs.ParseSeries("AnyShow")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := Deps{LibRoot: t.TempDir(), Now: func() time.Time { return time.Unix(0, 0) }}
	_, err = BuildPlan(ctx, deps, PlanInput{Ref: ref})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRunSteps_LogsAppliedAndFailedAtInfoLevel(t *testing.T) {
	dir := t.TempDir()
	src1 := filepath.Join(dir, "src1.mkv")
	if err := os.WriteFile(src1, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := []Step{
		{ID: "AAAA", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: src1, To: filepath.Join(dir, "dst1.mkv")},
		{ID: "BBBB", Kind: StepFileMove, Owner: Owner{Kind: OwnerEpisode}, From: filepath.Join(dir, "missing.mkv"), To: filepath.Join(dir, "dst2.mkv")},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	exec := &executor{
		deps:      Deps{Now: func() time.Time { return time.Unix(0, 0) }},
		in:        ApplyInput{Plan: Plan{Steps: steps}, Log: &stubLog{}},
		seriesDir: dir,
		log:       logger,
	}
	_, _, err := exec.runSteps(context.Background())
	if err == nil {
		t.Fatal("runSteps: nil err, want failure")
	}
	got := buf.String()
	if !strings.Contains(got, `msg="apply step done"`) {
		t.Errorf("missing applied step log line; got:\n%s", got)
	}
	if !strings.Contains(got, "stepID=AAAA") {
		t.Errorf("applied step log missing stepID; got:\n%s", got)
	}
}

func TestRemoveDirIfEmpty_RefusesNonEmpty(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "extras")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a hidden file; refuse should still apply.
	if err := os.WriteFile(filepath.Join(target, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := removeDirIfEmpty(target)
	if err != nil {
		t.Fatalf("removeDirIfEmpty: %v", err)
	}
	if removed {
		t.Errorf("removed = true, want false (non-empty dir)")
	}
	// Directory must still exist (refused to remove).
	if _, err := os.Stat(target); err != nil {
		t.Errorf("dir should still exist: %v", err)
	}
}

func TestRemoveDirIfEmpty_RemovesEmpty(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "extras")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	removed, err := removeDirIfEmpty(target)
	if err != nil {
		t.Fatalf("removeDirIfEmpty: %v", err)
	}
	if !removed {
		t.Errorf("removed = false, want true")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("dir should be removed: err=%v", err)
	}
}

func TestRemoveDirIfEmpty_MissingDirIsNoop(t *testing.T) {
	dir := t.TempDir()
	removed, err := removeDirIfEmpty(filepath.Join(dir, "nonexistent"))
	if err != nil {
		t.Fatalf("removeDirIfEmpty: %v", err)
	}
	if removed {
		t.Errorf("removed = true for missing dir")
	}
}

func TestPruneEmptyAncestors_RemovesUntilNonEmpty(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a sibling at root so the walk stops there instead of
	// touching root itself.
	if err := os.WriteFile(filepath.Join(root, "sibling.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := &executor{
		deps:      Deps{LibRoot: root},
		seriesDir: root,
	}
	exec.pruneEmptyAncestors(deep)
	if _, err := os.Stat(deep); !os.IsNotExist(err) {
		t.Errorf("c should be removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a", "b")); !os.IsNotExist(err) {
		t.Errorf("b should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Errorf("a should be removed")
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("root should still exist (boundary): %v", err)
	}
}

func TestPruneEmptyAncestors_StopsAtSeriesRoot(t *testing.T) {
	libRoot := t.TempDir()
	seriesRoot := filepath.Join(libRoot, "Show")
	season := filepath.Join(seriesRoot, "Season 1")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatal(err)
	}
	exec := &executor{
		deps:      Deps{LibRoot: libRoot},
		seriesDir: seriesRoot,
	}
	exec.pruneEmptyAncestors(season)
	if _, err := os.Stat(season); !os.IsNotExist(err) {
		t.Errorf("Season 1 should be removed: %v", err)
	}
	if _, err := os.Stat(seriesRoot); err != nil {
		t.Errorf("series root must be preserved: %v", err)
	}
}

func TestPruneEmptyAncestors_StopsAtLibRoot(t *testing.T) {
	libRoot := t.TempDir()
	show := filepath.Join(libRoot, "Show")
	if err := os.MkdirAll(show, 0o755); err != nil {
		t.Fatal(err)
	}
	// seriesDir intentionally a different path so the libRoot
	// boundary is the only thing protecting the lib root.
	exec := &executor{
		deps:      Deps{LibRoot: libRoot},
		seriesDir: filepath.Join(libRoot, "Other"),
	}
	exec.pruneEmptyAncestors(show)
	if _, err := os.Stat(show); !os.IsNotExist(err) {
		t.Errorf("Show should be removed: %v", err)
	}
	if _, err := os.Stat(libRoot); err != nil {
		t.Errorf("library root must be preserved: %v", err)
	}
}

func TestPruneEmptyAncestors_StopsAtInboxRoot(t *testing.T) {
	libRoot := t.TempDir()
	inboxRoot := t.TempDir()
	subdir := filepath.Join(inboxRoot, "ShowName")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	exec := &executor{
		deps:      Deps{LibRoot: libRoot, InboxRoot: inboxRoot},
		seriesDir: filepath.Join(libRoot, "ShowName"),
	}
	exec.pruneEmptyAncestors(subdir)
	if _, err := os.Stat(subdir); !os.IsNotExist(err) {
		t.Errorf("inbox subdir should be removed: %v", err)
	}
	if _, err := os.Stat(inboxRoot); err != nil {
		t.Errorf("inbox root must be preserved: %v", err)
	}
}

func TestPruneEmptyAncestors_SkipsExternalPath(t *testing.T) {
	libRoot := t.TempDir()
	inboxRoot := t.TempDir()
	external := t.TempDir() // outside both lib and inbox
	subdir := filepath.Join(external, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	exec := &executor{
		deps:      Deps{LibRoot: libRoot, InboxRoot: inboxRoot},
		seriesDir: filepath.Join(libRoot, "ShowName"),
	}
	exec.pruneEmptyAncestors(subdir)
	// external subdir must not be touched
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("external subdir must not be removed: %v", err)
	}
}

func TestPruneEmptyAncestors_StopsOnNonEmpty(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "season")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := &executor{
		deps:      Deps{LibRoot: root},
		seriesDir: root,
	}
	exec.pruneEmptyAncestors(target)
	if _, err := os.Stat(target); err != nil {
		t.Errorf("non-empty dir must be preserved: %v", err)
	}
}

func TestApplyPostStateMutations_CanonicalRenameUpdatesActivePath(t *testing.T) {
	// Regression: an active-only canonical-rename step (intent="move",
	// no Staged record) must rewrite Active.Path so future plans /
	// kura_show reflect the post-apply filesystem state. Previously
	// the post-state walk only handled the staged → active promotion
	// path, leaving Active.Path stale after a normalization rename.
	ep, _ := refs.NewEpisode(1, 1)
	oldPath := "Season 1/old.mkv"
	newPath := "Season 1/new.mkv"
	model := &series.Series{
		Episodes: map[refs.Episode]series.Episode{
			ep: {Active: &media.Record{
				Path:   oldPath,
				Source: media.SourceUnknown,
			}},
		},
	}
	plan := Plan{
		Steps: []Step{{
			ID:    "AAAA",
			Kind:  StepFileMove,
			Owner: Owner{Kind: OwnerEpisode, EpisodeRef: ep, EpisodeIntent: "move"},
			From:  oldPath,
			To:    newPath,
		}},
	}
	exec := &executor{in: ApplyInput{Plan: plan}}
	if err := exec.applyPostStateMutations(model); err != nil {
		t.Fatalf("applyPostStateMutations: %v", err)
	}
	got := model.Episodes[ep].Active
	if got == nil {
		t.Fatal("Active dropped")
	}
	if got.Path != newPath {
		t.Fatalf("Active.Path = %q, want %q (canonical destination)", got.Path, newPath)
	}
}

func TestPruneEmptyAncestors_AbsentDirIsNoop(t *testing.T) {
	root := t.TempDir()
	exec := &executor{
		deps:      Deps{LibRoot: root},
		seriesDir: root,
	}
	exec.pruneEmptyAncestors(filepath.Join(root, "nonexistent"))
	// No assertion — just must not panic / error.
}

func TestWriteTrashMetas_RecordPathIsSeriesRelative(t *testing.T) {
	// Regression: writeTrashMetas was passing step.To (trash bucket path)
	// to recordToTrashfile instead of step.From (original series-relative
	// path), causing TrashRestore to compute the wrong destination.
	libRoot := t.TempDir()
	ref, err := refs.ParseSeries("TestShow")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	ep, _ := refs.NewEpisode(1, 1)
	id := ulid.Make()

	originalPath := "Season 1/ep1.mp4"
	trashPath := ".kura/trash/" + id.String() + "/ep1.mp4"

	plan := Plan{
		Steps: []Step{{
			ID:   "TTTT",
			Kind: StepFileMove,
			Owner: Owner{
				Kind:            OwnerTrash,
				TrashID:         id.String(),
				OriginalEpisode: ep,
				Record: &ReplacedRecord{
					Path:  originalPath,
					Size:  1024,
					MTime: time.Unix(0, 0),
				},
			},
			From: originalPath,
			To:   trashPath,
		}},
	}
	exec := &executor{
		deps: Deps{
			LibRoot: libRoot,
			Now:     func() time.Time { return time.Unix(0, 0) },
		},
		in: ApplyInput{Ref: ref, Plan: plan},
	}

	if err := exec.writeTrashMetas(); err != nil {
		t.Fatalf("writeTrashMetas: %v", err)
	}

	meta, err := trashfile.Read(libRoot, ref, id)
	if err != nil {
		t.Fatalf("trashfile.Read: %v", err)
	}
	if meta.Record.Path != originalPath {
		t.Errorf("Record.Path = %q, want %q (series-relative pre-move path, not trash path)",
			meta.Record.Path, originalPath)
	}
}
