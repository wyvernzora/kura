//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

const crawlE2ELookback = "24h"

type crawlE2EBinaries struct {
	cli     string
	indexer string
}

type crawlE2EHarness struct {
	baseURL string
	dsn     string
	source  *crawlE2ESource
}

type crawlE2ESource struct {
	pages    map[int]string
	failPage atomic.Int64
	server   *httptest.Server
}

func TestEndToEndWorkflowCrawlCLI(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()

	bins := buildCrawlE2EBinaries(t, ctx)
	now := time.Now()

	t.Run("bounded chunk and idempotent replay", func(t *testing.T) {
		h := startCrawlE2EHarness(t, ctx, bins, map[int]string{
			1: fakeArchivePage(
				crawlE2ERow(101, now.Add(-time.Hour)),
				crawlE2ERow(102, now.Add(-2*time.Hour)),
				crawlE2ERow(103, now.Add(-3*time.Hour)),
			),
		}, 0)

		first := runCrawlCLI(t, ctx, bins.cli, h.baseURL,
			"crawl", "dmhy", "--count", "2", "--lookback", crawlE2ELookback, "--json")
		if first.err != nil {
			t.Fatalf("first crawl: %v\nstderr:\n%s", first.err, first.stderr)
		}
		firstResults := decodeCrawlResults(t, first.stdout)
		if len(firstResults) != 1 {
			t.Fatalf("first results = %d, want 1; output=%q", len(firstResults), first.stdout)
		}
		got := firstResults[0]
		if got.Posts != 2 || got.Batch.New != 2 || got.Batch.Duplicate != 0 ||
			!got.HasMore || got.NextCursor == "" || got.StopReason != api.CrawlStopPageBudget {
			t.Fatalf("first result = %+v, want 2 new posts and a resume cursor", got)
		}
		assertCrawlDatabase(t, ctx, h.dsn, 2, []string{"101", "102"})

		replay := runCrawlCLI(t, ctx, bins.cli, h.baseURL,
			"crawl", "dmhy", "--count", "2", "--lookback", crawlE2ELookback, "--json")
		if replay.err != nil {
			t.Fatalf("replay crawl: %v\nstderr:\n%s", replay.err, replay.stderr)
		}
		replayResults := decodeCrawlResults(t, replay.stdout)
		if len(replayResults) != 1 {
			t.Fatalf("replay results = %d, want 1; output=%q", len(replayResults), replay.stdout)
		}
		replayed := replayResults[0]
		if replayed.Posts != 2 || replayed.Batch.New != 0 || replayed.Batch.Duplicate != 2 ||
			replayed.NextCursor != got.NextCursor {
			t.Fatalf("replay result = %+v, want 2 duplicates and the same cursor", replayed)
		}
		assertCrawlDatabase(t, ctx, h.dsn, 2, []string{"101", "102"})
	})

	t.Run("client loop reaches lookback boundary", func(t *testing.T) {
		h := startCrawlE2EHarness(t, ctx, bins, map[int]string{
			1: fakeArchivePage(
				crawlE2ERow(201, now.Add(-time.Hour)),
				crawlE2ERow(202, now.Add(-2*time.Hour)),
				crawlE2ERow(203, now.Add(-3*time.Hour)),
				crawlE2ERow(204, now.Add(-48*time.Hour)),
			),
		}, 0)

		result := runCrawlCLI(t, ctx, bins.cli, h.baseURL,
			"crawl", "dmhy", "--count", "2", "--lookback", crawlE2ELookback, "--loop", "--json")
		if result.err != nil {
			t.Fatalf("loop crawl: %v\nstderr:\n%s", result.err, result.stderr)
		}
		results := decodeCrawlResults(t, result.stdout)
		if len(results) != 2 {
			t.Fatalf("loop results = %d, want 2; output=%q", len(results), result.stdout)
		}
		if first := results[0]; first.Posts != 2 || first.Batch.New != 2 ||
			!first.HasMore || first.NextCursor == "" || first.StopReason != api.CrawlStopPageBudget {
			t.Fatalf("first loop result = %+v", first)
		}
		if last := results[1]; last.Posts != 1 || last.Batch.New != 1 ||
			last.HasMore || last.NextCursor != "" || last.StopReason != api.CrawlStopLookbackBoundary {
			t.Fatalf("terminal loop result = %+v", last)
		}
		assertCrawlDatabase(t, ctx, h.dsn, 3, []string{"201", "202", "203"})
	})

	t.Run("failure reports cursor and resume completes", func(t *testing.T) {
		h := startCrawlE2EHarness(t, ctx, bins, map[int]string{
			1: fakeArchivePage(
				crawlE2ERow(301, now.Add(-time.Hour)),
				crawlE2ERow(302, now.Add(-2*time.Hour)),
			),
			2: fakeArchivePage(
				crawlE2ERow(303, now.Add(-3*time.Hour)),
				crawlE2ERow(304, now.Add(-4*time.Hour)),
			),
			3: fakeArchivePage(crawlE2ERow(305, now.Add(-48*time.Hour))),
		}, 2)

		failed := runCrawlCLI(t, ctx, bins.cli, h.baseURL,
			"crawl", "dmhy", "--count", "2", "--lookback", crawlE2ELookback, "--loop", "--json")
		if failed.err == nil {
			t.Fatalf("failed loop exited successfully; stdout=%q", failed.stdout)
		}
		failedResults := decodeCrawlResults(t, failed.stdout)
		if len(failedResults) != 1 || failedResults[0].NextCursor == "" || !failedResults[0].HasMore {
			t.Fatalf("failed loop results = %+v, want one committed chunk with cursor", failedResults)
		}
		cursor := failedResults[0].NextCursor
		resumeCommand := "kura crawl dmhy --count 2 --cursor " + cursor + " --lookback " + crawlE2ELookback
		if !strings.Contains(failed.stderr, resumeCommand) {
			t.Fatalf("stderr = %q, want resume command %q", failed.stderr, resumeCommand)
		}
		assertCrawlDatabase(t, ctx, h.dsn, 2, []string{"301", "302"})

		h.source.failPage.Store(0)
		resumed := runCrawlCLI(t, ctx, bins.cli, h.baseURL,
			"crawl", "dmhy", "--count", "2", "--cursor", cursor,
			"--lookback", crawlE2ELookback, "--loop", "--json")
		if resumed.err != nil {
			t.Fatalf("resumed crawl: %v\nstderr:\n%s", resumed.err, resumed.stderr)
		}
		resumedResults := decodeCrawlResults(t, resumed.stdout)
		if len(resumedResults) != 2 {
			t.Fatalf("resumed results = %d, want 2; output=%q", len(resumedResults), resumed.stdout)
		}
		if first := resumedResults[0]; first.Posts != 2 || first.Batch.New != 2 || !first.HasMore {
			t.Fatalf("first resumed result = %+v", first)
		}
		if last := resumedResults[1]; last.Posts != 0 || last.HasMore ||
			last.StopReason != api.CrawlStopLookbackBoundary {
			t.Fatalf("terminal resumed result = %+v", last)
		}
		assertCrawlDatabase(t, ctx, h.dsn, 4, []string{"301", "302", "303", "304"})
	})
}

func buildCrawlE2EBinaries(t *testing.T, ctx context.Context) crawlE2EBinaries {
	t.Helper()
	serviceRoot := repoRoot(t)
	monorepoRoot := filepath.Clean(filepath.Join(serviceRoot, "..", ".."))
	binDir := t.TempDir()
	bins := crawlE2EBinaries{
		cli:     filepath.Join(binDir, "kura"),
		indexer: filepath.Join(binDir, "kura-release-indexer"),
	}
	buildGoBinary(t, ctx, serviceRoot, bins.indexer, "./cmd/kura-release-indexer")
	buildGoBinary(t, ctx, filepath.Join(monorepoRoot, "cli"), bins.cli, "./cmd/kura")
	return bins
}

func buildGoBinary(t *testing.T, ctx context.Context, dir, output, pkg string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, pkg)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, stderr.String())
	}
}

func startCrawlE2EHarness(
	t *testing.T,
	ctx context.Context,
	bins crawlE2EBinaries,
	pages map[int]string,
	failPage int64,
) *crawlE2EHarness {
	t.Helper()

	source := &crawlE2ESource{pages: pages}
	source.failPage.Store(failPage)
	source.server = httptest.NewServer(source)
	t.Cleanup(source.server.Close)

	pg, err := tcpostgres.Run(ctx,
		"postgres:18-alpine",
		tcpostgres.WithDatabase("kura"),
		tcpostgres.WithUsername("kura"),
		tcpostgres.WithPassword("kura"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		if err := pg.Terminate(stopCtx); err != nil {
			t.Logf("terminate postgres: %v", err)
		}
	})
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}

	addr := crawlE2EFreeAddr(t)
	metricsAddr := crawlE2EFreeAddr(t)
	config := fmt.Sprintf(`[server]
addr = %q
metrics_addr = %q
log_level = "debug"

[sources.dmhy]
enabled = false
request_timeout = "5s"
url = %q
category = "2"
max_rps = 100
cache_ttl = "5m"
`, addr, metricsAddr, source.server.URL)
	configPath := filepath.Join(t.TempDir(), "release-indexer.toml")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.CommandContext(ctx, bins.indexer, "--config="+configPath)
	cmd.Env = append(os.Environ(), "KURA_RELEASES_DATABASE_URL="+dsn)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start release-indexer: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case err := <-done:
			if err != nil {
				t.Logf("release-indexer exited before cleanup: %v", err)
			}
			return
		default:
		}
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case err := <-done:
			if err != nil {
				t.Logf("release-indexer shutdown: %v", err)
			}
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})

	baseURL := "http://" + addr
	waitForCrawlE2EHealth(t, ctx, baseURL)
	return &crawlE2EHarness{
		baseURL: baseURL,
		dsn:     dsn,
		source:  source,
	}
}

func (s *crawlE2ESource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const prefix = "/topics/list/sort_id/2/page/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	page, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, prefix))
	if err != nil || page < 1 {
		http.Error(w, "invalid page", http.StatusBadRequest)
		return
	}
	if s.failPage.Load() == int64(page) {
		http.Error(w, "injected source failure", http.StatusServiceUnavailable)
		return
	}
	body, ok := s.pages[page]
	if !ok {
		body = fakeArchivePage()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, body)
}

type crawlCLIResult struct {
	stdout string
	stderr string
	err    error
}

func runCrawlCLI(
	t *testing.T,
	ctx context.Context,
	bin string,
	baseURL string,
	args ...string,
) crawlCLIResult {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "KURA_SERVER_URL="+baseURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return crawlCLIResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func decodeCrawlResults(t *testing.T, raw string) []api.CrawlResult {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var results []api.CrawlResult
	for {
		var result api.CrawlResult
		err := dec.Decode(&result)
		if errors.Is(err, io.EOF) {
			return results
		}
		if err != nil {
			t.Fatalf("decode crawl output %q: %v", raw, err)
		}
		results = append(results, result)
	}
}

func assertCrawlDatabase(
	t *testing.T,
	ctx context.Context,
	dsn string,
	wantCount int,
	wantSourceIDs []string,
) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Errorf("close postgres connection: %v", err)
		}
	}()

	var releases, rawItems, distinctInfohashes int
	err = conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM releases.releases),
			(SELECT count(*) FROM releases.raw_items),
			(SELECT count(DISTINCT infohash) FROM releases.raw_items)
	`).Scan(&releases, &rawItems, &distinctInfohashes)
	if err != nil {
		t.Fatalf("query crawl row counts: %v", err)
	}
	if releases != wantCount || rawItems != wantCount || distinctInfohashes != wantCount {
		t.Fatalf("database counts = releases:%d raw_items:%d distinct_infohashes:%d, want %d each",
			releases, rawItems, distinctInfohashes, wantCount)
	}

	rows, err := conn.Query(ctx, `SELECT source_id FROM releases.raw_items ORDER BY source_id`)
	if err != nil {
		t.Fatalf("query source IDs: %v", err)
	}
	defer rows.Close()
	var sourceIDs []string
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			t.Fatalf("scan source ID: %v", err)
		}
		sourceIDs = append(sourceIDs, sourceID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate source IDs: %v", err)
	}
	if strings.Join(sourceIDs, ",") != strings.Join(wantSourceIDs, ",") {
		t.Fatalf("source IDs = %v, want %v", sourceIDs, wantSourceIDs)
	}
}

func crawlE2ERow(sourceID int, publishedAt time.Time) string {
	const dmhyOffset = 8 * 60 * 60
	infohash := fmt.Sprintf("%040x", sourceID)
	published := publishedAt.In(time.FixedZone("DMHY", dmhyOffset)).Format("2006/01/02 15:04")
	return row(
		strconv.Itoa(sourceID),
		fmt.Sprintf("Crawl E2E Release %d", sourceID),
		infohash,
		"1GB",
		published,
		"udp://tracker.example/announce",
	)
}

func crawlE2EFreeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("release port %s: %v", addr, err)
	}
	return addr
}

func waitForCrawlE2EHealth(t *testing.T, ctx context.Context, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", http.NoBody)
		if err != nil {
			t.Fatalf("build health request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s did not become healthy within 60s", baseURL)
}
