package indexfile_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
)

// staticBuilder returns a RowBuilder that reports a fixed status for any
// directory, regardless of whether it has a series.json. Used by tests
// that exercise Rebuild paths without setting up real fixtures.
func staticBuilder(metadataPrefix string) indexfile.RowBuilder {
	return func(_ string, ref refs.Series, now time.Time) (indexfile.Row, error) {
		return indexfile.Row{
			Series:    ref,
			Metadata:  refs.Metadata(metadataPrefix + ":" + ref.String()),
			Title:     ref.String(),
			Status:    response.ListStatusComplete,
			UpdatedAt: now.UTC().Format(time.RFC3339),
		}, nil
	}
}

func TestSnapshot_NotReadyDuringColdRebuild(t *testing.T) {
	libRoot := t.TempDir()
	idx := indexfile.New(libRoot)

	// Use a builder that blocks until we say so, so we can observe the
	// "rebuilding && empty" state from outside.
	gate := make(chan struct{})
	builder := func(_ string, ref refs.Series, now time.Time) (indexfile.Row, error) {
		<-gate
		return indexfile.Row{
			Series:    ref,
			Title:     ref.String(),
			Status:    response.ListStatusComplete,
			UpdatedAt: now.UTC().Format(time.RFC3339),
		}, nil
	}

	if err := os.MkdirAll(filepath.Join(libRoot, "Bookworm"), 0o755); err != nil {
		t.Fatal(err)
	}

	idx.TriggerRebuild(context.Background(), libRoot, builder, coord.NewMutator("test"))

	// Wait briefly for the goroutine to flip the flag.
	deadline := time.Now().Add(500 * time.Millisecond)
	for !idx.Rebuilding() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !idx.Rebuilding() {
		close(gate)
		t.Fatal("Rebuilding never observed true")
	}

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
	idx := indexfile.New(libRoot)

	// Seed first rebuild synchronously.
	idx.TriggerRebuild(context.Background(), libRoot, staticBuilder("tvdb"), coord.NewMutator("seed"))
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatalf("WaitReady seed: %v", err)
	}

	// Block the second rebuild mid-flight; the prior snapshot should
	// keep serving.
	gate := make(chan struct{})
	builder := func(_ string, ref refs.Series, now time.Time) (indexfile.Row, error) {
		<-gate
		return indexfile.Row{
			Series:    ref,
			Title:     ref.String(),
			Status:    response.ListStatusComplete,
			UpdatedAt: now.UTC().Format(time.RFC3339),
		}, nil
	}
	idx.TriggerRebuild(context.Background(), libRoot, builder, coord.NewMutator("warm"))

	deadline := time.Now().Add(500 * time.Millisecond)
	for !idx.Rebuilding() && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if !idx.Rebuilding() {
		close(gate)
		t.Fatal("Rebuilding never observed true on warm rebuild")
	}

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
	idx := indexfile.New(libRoot)

	gate := make(chan struct{})
	calls := 0
	builder := func(_ string, ref refs.Series, now time.Time) (indexfile.Row, error) {
		calls++
		<-gate
		return indexfile.Row{Series: ref, Title: ref.String(), Status: response.ListStatusComplete}, nil
	}

	idx.TriggerRebuild(context.Background(), libRoot, builder, coord.NewMutator("a"))
	idx.TriggerRebuild(context.Background(), libRoot, builder, coord.NewMutator("b"))
	idx.TriggerRebuild(context.Background(), libRoot, builder, coord.NewMutator("c"))

	// Wait briefly to be sure all three calls are processed.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	if err := idx.WaitReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Builder should have been called once for the single in-flight
	// rebuild's walk (which sees one directory). Subsequent
	// TriggerRebuild calls during the in-flight rebuild are no-ops.
	if calls != 1 {
		t.Fatalf("builder calls = %d, want 1 (idempotent)", calls)
	}
}
