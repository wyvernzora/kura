package sweep

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/jobs"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/paths"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mustWritePlan(t *testing.T, libRoot string, ref refs.Series, token string, mtime time.Time) string {
	t.Helper()
	dir := paths.PlanDir(libRoot, ref)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := paths.PlanFile(libRoot, ref, token)
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return path
}

func TestSweepOnce_DeletesOldPlansKeepsRecent(t *testing.T) {
	root := t.TempDir()
	refA, _ := refs.ParseSeries("AlphaShow")
	refB, _ := refs.ParseSeries("BetaShow")
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	old := now.Add(-8 * 24 * time.Hour)
	young := now.Add(-1 * 24 * time.Hour)

	oldA := mustWritePlan(t, root, refA, "old1", old)
	youngA := mustWritePlan(t, root, refA, "young1", young)
	oldB := mustWritePlan(t, root, refB, "old2", old)

	sweepOnce(root, Config{LogRetention: 7 * 24 * time.Hour}, discardLogger(), now)

	if _, err := os.Stat(oldA); !os.IsNotExist(err) {
		t.Errorf("oldA should be gone: err=%v", err)
	}
	if _, err := os.Stat(oldB); !os.IsNotExist(err) {
		t.Errorf("oldB should be gone: err=%v", err)
	}
	if _, err := os.Stat(youngA); err != nil {
		t.Errorf("youngA should be kept: %v", err)
	}
}

func TestSweepOnce_SkipsDotDirsAndUnparseableNames(t *testing.T) {
	root := t.TempDir()
	// .kura at lib root must not be parsed as a series.
	if err := os.MkdirAll(filepath.Join(root, ".kura"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory with no .kura/reconcile/ — sweep must not error.
	if err := os.MkdirAll(filepath.Join(root, "Show"), 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	// No panic, no error path triggered.
	sweepOnce(root, Config{LogRetention: time.Hour}, discardLogger(), now)
}

func TestSweepOnce_IgnoresNonJSONLFiles(t *testing.T) {
	root := t.TempDir()
	ref, _ := refs.ParseSeries("Show")
	planDir := paths.PlanDir(root, ref)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stray file that isn't a plan log; mtime old enough to be deleted
	// IF the filter were missing.
	stray := filepath.Join(planDir, "notes.txt")
	if err := os.WriteFile(stray, []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(stray, old, old); err != nil {
		t.Fatal(err)
	}

	sweepOnce(root, Config{LogRetention: 7 * 24 * time.Hour}, discardLogger(), time.Now())

	if _, err := os.Stat(stray); err != nil {
		t.Errorf("stray non-jsonl file should be untouched: %v", err)
	}
}

func mustWriteJobLog(t *testing.T, libRoot, id string, mtime time.Time) string {
	t.Helper()
	dir := paths.JobsDir(libRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := paths.JobFile(libRoot, id)
	if err := os.WriteFile(path, []byte(`{"type":"header","jobId":"`+id+`"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	return path
}

func TestSweepOnce_DeletesOldJobLogsKeepsRecent(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	young := now.Add(-1 * 24 * time.Hour)

	oldFile := mustWriteJobLog(t, root, "01JOLDXXXXXXXXXXXXXXXXXXXX", old)
	youngFile := mustWriteJobLog(t, root, "01JNEWXXXXXXXXXXXXXXXXXXXX", young)

	sweepOnce(root, Config{LogRetention: 7 * 24 * time.Hour}, discardLogger(), now)

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("oldFile should be gone: err=%v", err)
	}
	if _, err := os.Stat(youngFile); err != nil {
		t.Errorf("youngFile should be kept: %v", err)
	}
}

func TestSweepOnce_PreservesActiveJobLogs(t *testing.T) {
	root := t.TempDir()
	registry := jobs.NewRegistry(context.Background(), jobs.Config{LibRoot: root}, nil)
	t.Cleanup(func() { registry.Shutdown(time.Second) })

	finish := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	ref, _ := refs.ParseSeries("ActiveShow")
	j := jobs.Submit(registry, context.Background(), jobs.KindScan, ref, func(ctx context.Context) (int, error) {
		defer wg.Done()
		<-finish
		return 0, nil
	})

	// File mtime is "now" but we lie to the sweep about its age via
	// Chtimes so the time bound says delete; the active-skip path is
	// what keeps it alive.
	path := paths.JobFile(root, j.ID())
	old := time.Now().Add(-30 * 24 * time.Hour)
	// Wait for the writer to flush header.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	sweepOnce(root, Config{LogRetention: 7 * 24 * time.Hour, Registry: registry}, discardLogger(), time.Now())

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("active job log should be preserved: %v", err)
	}

	close(finish)
	wg.Wait()
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestSweepOnce_IgnoresNonJSONLInJobsDir(t *testing.T) {
	root := t.TempDir()
	dir := paths.JobsDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(stray, []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(stray, old, old); err != nil {
		t.Fatal(err)
	}

	sweepOnce(root, Config{LogRetention: 7 * 24 * time.Hour}, discardLogger(), time.Now())

	if _, err := os.Stat(stray); err != nil {
		t.Errorf("stray non-jsonl file should be untouched: %v", err)
	}
}
