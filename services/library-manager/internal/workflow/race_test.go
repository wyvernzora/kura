package workflow_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/coord"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/domain/series"
	"github.com/wyvernzora/kura/services/library-manager/internal/storage/seriesfile"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// writeMedia creates a media file at <seriesRoot>/<rel> with the given
// body and returns its absolute path. Shared helper for stage / reconcile
// tests that need a file on disk to work with.
func writeMedia(t *testing.T, seriesRoot, rel, body string) string {
	t.Helper()
	abs := filepath.Join(seriesRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return abs
}

// seedSeries creates a fresh tracked series with no episodes ready to
// receive concurrent CAS writes. Returns the deps + series ref.
// Both the MCP coordinator (sync.Map) and the file-level CAS are
// exercised — the in-process mutex serializes the goroutines, the
// hash check would catch any drift if the mutex were absent.
func seedSeries(t *testing.T) (workflow.Deps, refs.Series) {
	t.Helper()
	root := t.TempDir()
	ref, err := refs.ParseSeries("Show A")
	if err != nil {
		t.Fatalf("ParseSeries: %v", err)
	}
	model := &series.Series{
		Ref:      ref,
		Metadata: refs.Metadata("tvdb:42"),
		Episodes: map[refs.Episode]series.Episode{},
	}
	if err := seriesfile.SaveCAS(root, model, coord.NewMutator("test_seed")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	deps := workflow.Deps{
		LibRoot:     root,
		Coordinator: coord.NewMCPCoordinator(),
		Now:         time.Now,
	}
	return deps, ref
}

// TestRace_ConcurrentResetSerializes drives many goroutines through
// Reset against the same series. Every call respects the per-series
// mutex (no two CAS writes overlap) AND every call lands cleanly
// (none surface ConflictError because the mutex prevents the race).
//
// If the in-process mutex were absent we'd see ConflictErrors when
// two goroutines pass the hash check simultaneously and the slower
// one's rename clobbers the post-write verify.
func TestRace_ConcurrentResetSerializes(t *testing.T) {
	deps, ref := seedSeries(t)

	var wg sync.WaitGroup
	var conflicts atomic.Int32
	var successes atomic.Int32

	for range 20 {
		wg.Go(func() {
			_, err := workflow.Reset(context.Background(), deps, workflow.ResetInput{
				Ref: ref,
				All: true,
			})
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, &coord.ConflictError{}):
				conflicts.Add(1)
			}
		})
	}
	wg.Wait()

	if conflicts.Load() != 0 {
		t.Fatalf("conflicts = %d, want 0 (in-process mutex must serialize)", conflicts.Load())
	}
	if successes.Load() != 20 {
		t.Fatalf("successes = %d, want 20", successes.Load())
	}
}

// TestRace_PeerWriteCausesPreWriteConflict simulates an external peer
// (different process) writing series.json between our Load and our
// SaveCAS. The CAS pre-write check detects the drift and surfaces a
// ConflictError carrying the peer's mutator identity.
func TestRace_PeerWriteCausesPreWriteConflict(t *testing.T) {
	deps, ref := seedSeries(t)

	// Load to capture the original hash.
	model, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	originalHash := model.Hash

	// Peer writes between our load and our save.
	peer, err := seriesfile.Load(deps.LibRoot, ref)
	if err != nil {
		t.Fatalf("peer Load: %v", err)
	}
	peer.LastScanned = peer.LastScanned.Add(42 * time.Second)
	peerMutator := coord.Mutator{
		Op:   "peer_op",
		PID:  99999,
		Host: "test-host",
		At:   time.Now().UTC().Truncate(time.Second),
	}
	if err := seriesfile.SaveCAS(deps.LibRoot, peer, peerMutator); err != nil {
		t.Fatalf("peer SaveCAS: %v", err)
	}

	// Our SaveCAS still expects originalHash; should conflict.
	model.Hash = originalHash
	model.LastScanned = model.LastScanned.Add(time.Second)
	err = seriesfile.SaveCAS(deps.LibRoot, model, coord.Mutator{
		Op: "ours", PID: os.Getpid(), Host: "test-host", At: time.Now().UTC().Truncate(time.Second),
	})
	var conflict *coord.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	if conflict.Phase != "pre_write" {
		t.Fatalf("Phase = %q, want pre_write", conflict.Phase)
	}
	if conflict.Mutator.PID != 99999 || conflict.Mutator.Op != "peer_op" {
		t.Fatalf("conflict mutator = %+v, want peer", conflict.Mutator)
	}
}

// TestRace_RetryRecoversFromConflict drives the full workflow path
// (Stage) where a peer writes mid-attempt; the coord retry runs the
// callback again and succeeds on the second pass.
func TestRace_RetryRecoversFromConflict(t *testing.T) {
	deps, ref := seedSeries(t)

	// Inject a peer mutation by a one-shot trigger inside a callback
	// the test controls. We bypass the workflow shape and test the
	// retry primitive directly so the assertion is precise.
	originalHash := func() string {
		m, _ := seriesfile.Load(deps.LibRoot, ref)
		return m.Hash
	}()

	var attempts int
	err := coord.RetryOnConflict(2, func() error {
		attempts++
		model, err := seriesfile.Load(deps.LibRoot, ref)
		if err != nil {
			return err
		}
		// First attempt: peer writes between Load and SaveCAS.
		if attempts == 1 {
			peer, _ := seriesfile.Load(deps.LibRoot, ref)
			peer.LastScanned = peer.LastScanned.Add(time.Second)
			_ = seriesfile.SaveCAS(deps.LibRoot, peer, coord.Mutator{
				Op: "peer", PID: 99999, Host: "test-host", At: time.Now().UTC().Truncate(time.Second),
			})
		}
		model.LastScanned = model.LastScanned.Add(2 * time.Second)
		return seriesfile.SaveCAS(deps.LibRoot, model, coord.Mutator{
			Op: "ours", PID: os.Getpid(), Host: "test-host", At: time.Now().UTC().Truncate(time.Second),
		})
	})
	if err != nil {
		t.Fatalf("RetryOnConflict: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one conflict + one retry success)", attempts)
	}

	// Final hash differs from original.
	final, _ := seriesfile.Load(deps.LibRoot, ref)
	if final.Hash == originalHash {
		t.Fatal("hash unchanged after retry-success")
	}
}
