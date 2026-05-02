package sweep

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
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

	sweepOnce(root, Config{PlanTTL: 7 * 24 * time.Hour}, discardLogger(), now)

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
	sweepOnce(root, Config{PlanTTL: time.Hour}, discardLogger(), now)
}

func TestJitteredInterval_StaysWithinWindow(t *testing.T) {
	const (
		interval = time.Hour
		jitter   = 5 * time.Minute
	)
	for range 100 {
		d := jitteredInterval(interval, jitter)
		if d < interval-jitter || d > interval+jitter {
			t.Fatalf("jitteredInterval = %v, want within [%v, %v]", d, interval-jitter, interval+jitter)
		}
	}
}

func TestJitteredInterval_ZeroJitterIsExactInterval(t *testing.T) {
	if got := jitteredInterval(time.Hour, 0); got != time.Hour {
		t.Errorf("jitteredInterval(1h, 0) = %v, want 1h", got)
	}
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

	sweepOnce(root, Config{PlanTTL: 7 * 24 * time.Hour}, discardLogger(), time.Now())

	if _, err := os.Stat(stray); err != nil {
		t.Errorf("stray non-jsonl file should be untouched: %v", err)
	}
}
