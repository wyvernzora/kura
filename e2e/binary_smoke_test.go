//go:build e2e

package e2e

import (
	"net/http"
	"testing"
	"time"
)

// TestE2EBinary_Smoke validates the new subprocess infrastructure end
// to end: build the kura-e2e binary, start a daemon against an
// ephemeral libRoot, hit /health + /library, and shut down cleanly.
//
// Runs once per process; subsequent scenario tests reuse the cached
// binary via buildOnce. This test is not parallel — it absorbs the
// build cost for the whole suite.
func TestE2EBinary_Smoke(t *testing.T) {
	libRoot := t.TempDir()
	inboxRoot := t.TempDir()
	b := startDaemon(t, libRoot, inboxRoot)

	resp, err := http.Get(b.url + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status: got %d want 200", resp.StatusCode)
	}

	resp2, err := http.Get(b.url + "/api/v1/library")
	if err != nil {
		t.Fatalf("GET /library: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("library status: got %d want 200", resp2.StatusCode)
	}

	// Belt-and-suspenders: explicit stop + verify it returns within
	// the 5s grace window. t.Cleanup also calls stop; sync.Once
	// guards re-entry.
	done := make(chan struct{})
	go func() {
		b.stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("daemon did not stop within 7s")
	}
}
