//go:build smoke

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestSmokeScheduledCrawl(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("takuhai"),
		tcpostgres.WithUsername("takuhai"),
		tcpostgres.WithPassword("takuhai"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = container.Terminate(stopCtx)
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}

	listing, err := os.ReadFile(filepath.Join("..", "..", "sources", "nyaa", "testdata", "live-listing-p2.html"))
	if err != nil {
		t.Fatalf("read Nyaa listing fixture: %v", err)
	}
	noResults, err := os.ReadFile(filepath.Join("..", "..", "sources", "nyaa", "testdata", "live-no-results.html"))
	if err != nil {
		t.Fatalf("read Nyaa no-results fixture: %v", err)
	}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("p") == "1" {
			_, _ = w.Write(listing)
			return
		}
		_, _ = w.Write(noResults)
	}))
	defer source.Close()

	binPath := filepath.Join(t.TempDir(), "release-indexer-smoke")
	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	build.Stderr = os.Stderr
	if out, err := build.Output(); err != nil {
		t.Fatalf("build release-indexer binary: %v\n%s", err, out)
	}

	addr := "127.0.0.1:" + freePort(t)
	configPath := filepath.Join(t.TempDir(), "release-indexer.toml")
	configBody := fmt.Sprintf(`
[server]
addr = %q

[sources.nyaa]
interval = "1h"
timeout = "30s"
url = %q
category = "1_4"
max_rps = 0
`, addr, source.URL)
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.CommandContext(ctx, binPath, "--config="+configPath)
	cmd.Env = append(os.Environ(), "KURA_RELEASES_DATABASE_URL="+dsn)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start release-indexer: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	baseURL := "http://" + addr
	waitHealthy(t, ctx, baseURL+"/healthz", 60*time.Second)
	waitForAvailableRelease(t, ctx, baseURL+"/queue/stats")

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal release-indexer: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("release-indexer shutdown: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("release-indexer did not stop within 15s")
	}
}

func waitForAvailableRelease(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var stats map[string]int
			decodeErr := json.NewDecoder(resp.Body).Decode(&stats)
			resp.Body.Close()
			if decodeErr == nil && stats["available"] > 0 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s did not report a scheduled-crawl release within 30s", url)
}
