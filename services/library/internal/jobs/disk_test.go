package jobs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/jobs"
	"github.com/wyvernzora/kura/services/library/internal/storage/jobfile"
	"github.com/wyvernzora/kura/services/library/internal/storage/paths"
)

// TestPersistedJobLogsToDisk asserts that a registry configured with
// a LibRoot writes a JSONL file containing header + progress +
// terminal lines for each job.
func TestPersistedJobLogsToDisk(t *testing.T) {
	libRoot := t.TempDir()
	r := newTestRegistry(t, jobs.Config{LibRoot: libRoot})

	j := jobs.Submit(r, context.Background(), jobs.KindScan, mustSeries(t, "show-disk"), func(ctx context.Context) (int, error) {
		return 7, nil
	})
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	id := j.ID()
	parsed, err := jobfile.Read(libRoot, id)
	if err != nil {
		t.Fatalf("jobfile.Read: %v", err)
	}
	if parsed.Header.JobID != id || parsed.Header.Kind != "scan" {
		t.Fatalf("header = %+v", parsed.Header)
	}
	if parsed.Terminal == nil || parsed.Terminal.State != "succeeded" {
		t.Fatalf("terminal = %+v", parsed.Terminal)
	}
	if string(parsed.Terminal.Result) != "7" {
		t.Fatalf("terminal.Result = %s, want 7", parsed.Terminal.Result)
	}
}

// TestGetFallsBackToDiskAfterEviction simulates the registry losing
// its in-memory entry (via TTL or restart) and asserts Get reads
// from disk transparently.
func TestGetFallsBackToDiskAfterEviction(t *testing.T) {
	libRoot := t.TempDir()
	r := newTestRegistry(t, jobs.Config{LibRoot: libRoot, Retention: time.Millisecond, ReaperInterval: 5 * time.Millisecond})

	j := jobs.Submit(r, context.Background(), jobs.KindScan, mustSeries(t, "show-evict"), func(ctx context.Context) (int, error) {
		return 1, nil
	})
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	id := j.ID()

	// Wait for the reaper to drop the in-memory entry.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err := r.Get(id)
		if err == nil {
			// Still in memory — keep waiting.
			time.Sleep(20 * time.Millisecond)
			continue
		}
		break
	}

	// Now the disk fallback should kick in.
	got, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get post-eviction: %v", err)
	}
	if got.State() != jobs.StatusSucceeded {
		t.Fatalf("State = %v, want Succeeded", got.State())
	}
	if got.ID() != id {
		t.Fatalf("ID = %q, want %q", got.ID(), id)
	}
}

// TestGetSynthesizesShutdownForOrphanLog covers a JSONL file with no
// terminal line (e.g. kura crashed mid-job). Get returns a synthesized
// failed terminal with kind=shutdown.
func TestGetSynthesizesShutdownForOrphanLog(t *testing.T) {
	libRoot := t.TempDir()
	r := newTestRegistry(t, jobs.Config{LibRoot: libRoot})

	id := "01JORPHAN000000000000000000"
	w, err := jobfile.Create(libRoot, jobfile.HeaderLine{
		JobID:     id,
		Kind:      "scan",
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State() != jobs.StatusFailed {
		t.Fatalf("State = %v, want Failed", got.State())
	}
	if !jobs.IsShutdownError(got.Err()) {
		t.Fatalf("Err = %v, want shutdown sentinel", got.Err())
	}
}

// TestGetReturnsNotFoundWhenAbsent confirms the disk lookup propagates
// fs.ErrNotExist as JobNotFoundError.
func TestGetReturnsNotFoundWhenAbsent(t *testing.T) {
	libRoot := t.TempDir()
	r := newTestRegistry(t, jobs.Config{LibRoot: libRoot})
	_, err := r.Get("01JNOPE0000000000000000000")
	var nf *jobs.JobNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want JobNotFoundError", err)
	}
}

// TestActiveIDsReturnsRunningSet exercises the sweep handoff API.
func TestActiveIDsReturnsRunningSet(t *testing.T) {
	r := newTestRegistry(t, jobs.Config{})
	finish := make(chan struct{})
	j := jobs.Submit(r, context.Background(), jobs.KindScan, mustSeries(t, "show-active"), func(ctx context.Context) (int, error) {
		<-finish
		return 0, nil
	})

	active := r.ActiveIDs()
	if _, ok := active[j.ID()]; !ok {
		t.Fatalf("ActiveIDs missing in-flight job; got %v", active)
	}
	close(finish)
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

// TestPersistedFileHasUlidName confirms the ULID swap reaches the
// filesystem path.
func TestPersistedFileHasUlidName(t *testing.T) {
	libRoot := t.TempDir()
	r := newTestRegistry(t, jobs.Config{LibRoot: libRoot})
	j := jobs.Submit(r, context.Background(), jobs.KindScan, mustSeries(t, "show-ulid"), func(ctx context.Context) (int, error) {
		return 0, nil
	})
	if _, err := j.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	jobsDir := paths.JobsDir(libRoot)
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", jobsDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file; got %d", len(entries))
	}
	name := entries[0].Name()
	if !strings.HasSuffix(name, ".jsonl") {
		t.Fatalf("name = %q, want *.jsonl", name)
	}
	if base := strings.TrimSuffix(name, ".jsonl"); len(base) != 26 {
		t.Fatalf("base = %q (len %d), want 26-char ULID", base, len(base))
	}
	// Also assert the expected absolute path exists.
	if _, err := os.Stat(filepath.Join(jobsDir, j.ID()+".jsonl")); err != nil {
		t.Fatalf("expected file at %s/%s.jsonl: %v", jobsDir, j.ID(), err)
	}
}
