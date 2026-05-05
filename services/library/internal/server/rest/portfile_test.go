package rest

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestServe_WritesPortFile starts Serve with addr ":0" + a port-file
// path and verifies the file gets written with the resolved port
// before any handler is hit. Cancels the context to shut the server.
func TestServe_WritesPortFile(t *testing.T) {
	srv := newTestServer(t)

	dir := t.TempDir()
	portFile := filepath.Join(dir, "port")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, "127.0.0.1:0", ServeOptions{PortFile: portFile}, srv)
	}()

	// Wait for port file to appear (up to 2s).
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(portFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("port file %s never appeared", portFile)
		}
		time.Sleep(20 * time.Millisecond)
	}

	buf, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("read port file: %v", err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(buf)))
	if err != nil {
		t.Fatalf("parse port file %q: %v", string(buf), err)
	}
	if port <= 0 || port > 65535 {
		t.Errorf("port %d out of range", port)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down within 5s")
	}
}
