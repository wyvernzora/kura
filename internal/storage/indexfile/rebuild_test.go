package indexfile_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

func TestSnapshot_NotReadyDuringColdRebuild(t *testing.T) {
	libRoot := t.TempDir()
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})

	gate := make(chan struct{})
	idx.SetEntryBuilderForTest(func(_ context.Context, _ string, ref refs.Series) (indexfile.Entry, error) {
		<-gate
		return indexfile.Entry{Series: ref}, nil
	})
	if err := os.MkdirAll(filepath.Join(libRoot, "Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}

	idx.TriggerRebuild(context.Background(), "test")
	waitFor(t, func() bool { return idx.Rebuilding() })

	rows, err := idx.Snapshot()
	if !errors.Is(err, indexfile.ErrNotReady) {
		t.Fatalf("Snapshot during cold rebuild = (%v, %v); want ErrNotReady", rows, err)
	}

	close(gate)
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	rows, err = idx.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot post-rebuild: %v", err)
	}
	if len(rows) != 1 || rows[0].Title != "Bookworm" {
		t.Fatalf("rows = %+v, want [Bookworm]", rows)
	}
}

func TestSnapshot_ReadyDuringWarmRebuild(t *testing.T) {
	libRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(libRoot, "Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})

	if err := idx.RebuildNow(context.Background(), "seed"); err != nil {
		t.Fatalf("RebuildNow seed: %v", err)
	}

	gate := make(chan struct{})
	idx.SetEntryBuilderForTest(func(_ context.Context, _ string, ref refs.Series) (indexfile.Entry, error) {
		<-gate
		return indexfile.Entry{Series: ref}, nil
	})
	idx.TriggerRebuild(context.Background(), "warm")
	waitFor(t, func() bool { return idx.Rebuilding() })

	rows, err := idx.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot during warm rebuild: %v (want existing rows)", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d during warm rebuild, want 1 (existing snapshot)", len(rows))
	}
	close(gate)
	_ = idx.WaitReady(context.Background())
}

func TestTriggerRebuild_IsIdempotent(t *testing.T) {
	libRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(libRoot, "Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx := indexfile.New(libRoot, indexfile.Config{BuildOptions: indexfile.DefaultBuildOptions()})

	gate := make(chan struct{})
	var calls atomic.Int32
	idx.SetEntryBuilderForTest(func(_ context.Context, _ string, ref refs.Series) (indexfile.Entry, error) {
		calls.Add(1)
		<-gate
		return indexfile.Entry{Series: ref}, nil
	})

	idx.TriggerRebuild(context.Background(), "a")
	idx.TriggerRebuild(context.Background(), "b")
	idx.TriggerRebuild(context.Background(), "c")
	time.Sleep(50 * time.Millisecond)
	close(gate)
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("builder calls = %d, want 1 (idempotent)", calls.Load())
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
