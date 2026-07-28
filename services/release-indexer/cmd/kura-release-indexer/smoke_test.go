//go:build smoke

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

const smokeHexInfohash = "0123456789abcdef0123456789abcdef01234567"

func TestSmoke(t *testing.T) {
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

	binPath := filepath.Join(t.TempDir(), "takuhai-smoke")
	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".")
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	build.Stderr = os.Stderr
	if out, err := build.Output(); err != nil {
		t.Fatalf("build takuhai binary: %v\n%s", err, out)
	}

	addr := "127.0.0.1:" + freePort(t)
	metricsAddr := "127.0.0.1:" + freePort(t)
	baseURL := "http://" + addr
	configPath := filepath.Join(t.TempDir(), "release-indexer.toml")
	cfg := "[server]\naddr = \"" + addr + "\"\nmetrics_addr = \"" + metricsAddr + "\"\n"
	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_ = startBinary(t, ctx, binPath, configPath, dsn)
	waitHealthy(t, ctx, baseURL+"/healthz", 60*time.Second)

	t.Run("ingest-claim-submit-stats", func(t *testing.T) {
		ingestBody := mustJSON(t, map[string]any{
			"posts": []api.RawPost{{
				Source:      api.SourceDMHY,
				SourceID:    "smoke-1",
				Title:       "Smoke Test Release",
				Magnet:      "magnet:?xt=urn:btih:" + smokeHexInfohash + "&tr=udp://smoke:80",
				PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				SizeBytes:   1234,
			}},
		})
		resp, err := http.Post(baseURL+"/api/v1/releases/ingest", "application/json", bytes.NewReader(ingestBody))
		if err != nil {
			t.Fatalf("POST /ingest: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST /ingest = %d, want 200", resp.StatusCode)
		}
		var summary api.IngestSummary
		if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
			t.Fatalf("decode /ingest summary: %v", err)
		}
		if summary.Batch.New != 1 || summary.Queue.Available < 1 {
			t.Fatalf("/ingest summary = %+v, want batch.new=1 and queue.available>=1", summary)
		}

		magnetResp, err := http.Get(baseURL + "/api/v1/releases/" + smokeHexInfohash + "/magnet")
		if err != nil {
			t.Fatalf("GET /api/v1/releases/{infohash}/magnet: %v", err)
		}
		defer magnetResp.Body.Close()
		if magnetResp.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/v1/releases/{infohash}/magnet = %d, want 200", magnetResp.StatusCode)
		}
		var magnet struct {
			Infohash string `json:"infohash"`
			Magnet   string `json:"magnet"`
		}
		if err := json.NewDecoder(magnetResp.Body).Decode(&magnet); err != nil {
			t.Fatalf("decode /api/v1/releases/{infohash}/magnet: %v", err)
		}
		if magnet.Infohash != smokeHexInfohash || magnet.Magnet != "magnet:?xt=urn:btih:"+smokeHexInfohash+"&tr=udp://smoke:80" {
			t.Fatalf("/api/v1/releases/{infohash}/magnet = %+v, want stored magnet", magnet)
		}

		claimResp, err := http.Post(baseURL+"/api/v1/releases/queue/claim", "application/json", bytes.NewReader(mustJSON(t, map[string]any{
			"limit": 5, "leaseSeconds": 60,
		})))
		if err != nil {
			t.Fatalf("POST /queue/claim: %v", err)
		}
		defer claimResp.Body.Close()
		if claimResp.StatusCode != http.StatusOK {
			t.Fatalf("POST /queue/claim = %d, want 200", claimResp.StatusCode)
		}
		var claim struct {
			Items []struct {
				Infohash   string `json:"infohash"`
				ClaimToken int64  `json:"claimToken"`
			} `json:"items"`
		}
		if err := json.NewDecoder(claimResp.Body).Decode(&claim); err != nil {
			t.Fatalf("decode /queue/claim: %v", err)
		}
		if len(claim.Items) != 1 || claim.Items[0].Infohash != smokeHexInfohash || claim.Items[0].ClaimToken == 0 {
			t.Fatalf("/api/v1/releases/queue/claim = %+v, want smoke release with token", claim.Items)
		}

		submitResp, err := http.Post(baseURL+"/api/v1/releases/queue/submit", "application/json", bytes.NewReader(mustJSON(t, map[string]any{
			"infohash":   claim.Items[0].Infohash,
			"claimToken": claim.Items[0].ClaimToken,
			"status":     "matched",
			"ref":        "tvdb:12345",
			"confidence": 0.99,
		})))
		if err != nil {
			t.Fatalf("POST /submit: %v", err)
		}
		defer submitResp.Body.Close()
		if submitResp.StatusCode != http.StatusOK {
			t.Fatalf("POST /submit = %d, want 200", submitResp.StatusCode)
		}

		releaseResp, err := http.Get(baseURL + "/api/v1/releases/" + smokeHexInfohash)
		if err != nil {
			t.Fatalf("GET /releases/{infohash}: %v", err)
		}
		defer releaseResp.Body.Close()
		if releaseResp.StatusCode != http.StatusOK {
			t.Fatalf("GET /releases/{infohash} = %d, want 200", releaseResp.StatusCode)
		}
		var release struct {
			Infohash    string `json:"infohash"`
			MatchStatus string `json:"matchStatus"`
			RawItems    []struct {
				SourceID string `json:"sourceId"`
			} `json:"rawItems"`
			MatchEvents []struct {
				Status string `json:"status"`
			} `json:"matchEvents"`
		}
		if err := json.NewDecoder(releaseResp.Body).Decode(&release); err != nil {
			t.Fatalf("decode /releases/{infohash}: %v", err)
		}
		if release.Infohash != smokeHexInfohash || release.MatchStatus != "matched" || len(release.RawItems) != 1 || len(release.MatchEvents) != 1 {
			t.Fatalf("/api/v1/releases/{infohash} = %+v, want matched release detail", release)
		}

		statsResp, err := http.Get(baseURL + "/api/v1/releases/queue/stats")
		if err != nil {
			t.Fatalf("GET /queue/stats: %v", err)
		}
		defer statsResp.Body.Close()
		var stats map[string]int
		if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
			t.Fatalf("decode /queue/stats: %v", err)
		}
		if stats["matched"] != 1 || stats["available"] != 0 {
			t.Fatalf("/api/v1/releases/queue/stats = %+v, want matched=1 available=0", stats)
		}
	})

	t.Run("old-worker-path-removed", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/worker/claim", "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatalf("POST /worker/claim: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("POST /worker/claim = %d, want 404", resp.StatusCode)
		}
	})

	// The listener split is a deploy-shape property: both halves are
	// wired in main, so only a booted binary can show that a scrape of
	// /metrics does not also reach ingest, claim, and submit.
	t.Run("metrics-served-off-the-api-listener", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics on API listener: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET /metrics on API listener = %d, want 404", resp.StatusCode)
		}

		mResp, err := http.Get("http://" + metricsAddr + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics on metrics listener: %v", err)
		}
		defer mResp.Body.Close()
		if mResp.StatusCode != http.StatusOK {
			t.Fatalf("GET /metrics on metrics listener = %d, want 200", mResp.StatusCode)
		}

		// And the write API is not reachable from the metrics listener.
		iResp, err := http.Post("http://"+metricsAddr+"/api/v1/releases/ingest",
			"application/json", bytes.NewReader([]byte(`{"posts":[]}`)))
		if err != nil {
			t.Fatalf("POST ingest on metrics listener: %v", err)
		}
		defer iResp.Body.Close()
		if iResp.StatusCode != http.StatusNotFound {
			t.Fatalf("POST ingest on metrics listener = %d, want 404", iResp.StatusCode)
		}
	})

	t.Run("suite-headers", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/api/v1/releases")
		if err != nil {
			t.Fatalf("GET /api/v1/releases: %v", err)
		}
		defer resp.Body.Close()
		if got := resp.Header.Get("X-Kura-Version"); got == "" {
			t.Error("X-Kura-Version is empty, want the build version")
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	})

	t.Run("bind-fast", func(t *testing.T) {
		second := exec.CommandContext(ctx, binPath, "--config="+configPath)
		second.Env = append(os.Environ(), "KURA_RELEASES_DATABASE_URL="+dsn)
		if err := second.Start(); err != nil {
			t.Fatalf("start second instance: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- second.Wait() }()
		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("second instance on busy addr %s exited 0, want non-zero", addr)
			}
		case <-time.After(15 * time.Second):
			_ = second.Process.Kill()
			t.Fatalf("second instance on busy addr %s did not exit within 15s", addr)
		}
	})
}

func startBinary(t *testing.T, ctx context.Context, binPath, configPath, dsn string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(ctx, binPath, "--config="+configPath)
	cmd.Env = append(os.Environ(), "KURA_RELEASES_DATABASE_URL="+dsn)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start takuhai binary: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd
}

func waitHealthy(t *testing.T, ctx context.Context, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("binary never became healthy at %s within %s", url, timeout)
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split free port: %v", err)
	}
	return port
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
