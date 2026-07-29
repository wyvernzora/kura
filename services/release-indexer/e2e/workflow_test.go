//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

const (
	matchInfohash    = "0123456789abcdef0123456789abcdef01234567"
	matchMagnet      = "magnet:?xt=urn:btih:" + matchInfohash + "&tr=udp://tracker.match/announce&tr=http://tracker.common/announce"
	suppressInfohash = "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	exhaustInfohash  = "1111111111111111111111111111111111111111"
	unknownInfohash  = "ffffffffffffffffffffffffffffffffffffffff"
)

func TestEndToEndWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	nw, err := tcnetwork.New(ctx)
	if err != nil {
		t.Fatalf("create docker network: %v", err)
	}
	t.Cleanup(func() {
		removeCtx, removeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer removeCancel()
		if err := nw.Remove(removeCtx); err != nil {
			t.Logf("cleanup network: %v", err)
		}
	})

	pg := startPostgres(t, ctx, nw)
	startFakeDMHY(t, ctx, nw)
	indexerURL := startIndexer(t, ctx, nw, pg)
	waitForAvailable(t, indexerURL, 3)

	matchToken := claimOne(t, indexerURL, matchInfohash, 30)
	assertSubmitStatus(t, indexerURL, http.StatusConflict, map[string]any{
		"infohash":   matchInfohash,
		"claimToken": matchToken - 1,
		"status":     "matched",
		"ref":        "tvdb:12345",
		"confidence": 0.5,
	})
	assertSubmitStatus(t, indexerURL, http.StatusBadRequest, map[string]any{
		"infohash":   matchInfohash,
		"claimToken": matchToken,
		"status":     "defer",
	})
	submitOK(t, indexerURL, map[string]any{
		"infohash":   matchInfohash,
		"claimToken": matchToken,
		"status":     "matched",
		"ref":        "tvdb:12345",
		"confidence": 0,
		"reason":     "e2e exact fixture match",
	})

	suppressToken := claimOne(t, indexerURL, suppressInfohash, 30)
	submitOK(t, indexerURL, map[string]any{
		"infohash":   suppressInfohash,
		"claimToken": suppressToken,
		"status":     "suppressed",
		"reason":     "e2e not wanted",
	})

	exhaustToken := claimOne(t, indexerURL, exhaustInfohash, 1)
	submitOK(t, indexerURL, map[string]any{
		"infohash":   exhaustInfohash,
		"claimToken": exhaustToken,
		"status":     "unmatched",
		"reason":     "e2e first miss",
	})
	assertNoClaim(t, indexerURL, "unmatched submit keeps the lease until timeout")
	time.Sleep(1500 * time.Millisecond)
	exhaustToken = claimOne(t, indexerURL, exhaustInfohash, 1)
	submitOK(t, indexerURL, map[string]any{
		"infohash":   exhaustInfohash,
		"claimToken": exhaustToken,
		"status":     "unmatched",
		"reason":     "e2e second miss exhausts",
	})

	stats := queueStats(t, indexerURL)
	if stats.Matched != 1 || stats.Suppressed != 1 || stats.Exhausted != 1 || stats.Available != 0 || stats.Leased != 0 {
		t.Fatalf("queue stats = %+v, want matched=1 suppressed=1 exhausted=1 available=0 leased=0", stats)
	}

	assertConsumerAPI(t, ctx, indexerURL, matchMagnet)
}

func startPostgres(t *testing.T, ctx context.Context, nw *testcontainers.DockerNetwork) *tcpostgres.PostgresContainer {
	t.Helper()
	pg, err := tcpostgres.Run(ctx,
		"postgres:18-alpine",
		tcpostgres.WithDatabase("kura"),
		tcpostgres.WithUsername("kura"),
		tcpostgres.WithPassword("kura"),
		tcnetwork.WithNetwork([]string{"postgres"}, nw),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = pg.Terminate(stopCtx)
	})
	return pg
}

func startFakeDMHY(t *testing.T, ctx context.Context, nw *testcontainers.DockerNetwork) testcontainers.Container {
	t.Helper()
	dir := fakeDMHYContext(t)
	c, err := testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:        dir,
			Dockerfile:     "Dockerfile",
			Repo:           "indexer-e2e-dmhy",
			Tag:            "latest",
			BuildLogWriter: os.Stderr,
		}),
		tcnetwork.WithNetwork([]string{"dmhy"}, nw),
		testcontainers.WithExposedPorts("80/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/topics/list/sort_id/2/page/1").WithPort("80/tcp").WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		t.Fatalf("start fake dmhy container: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = c.Terminate(stopCtx)
	})
	return c
}

func startIndexer(t *testing.T, ctx context.Context, nw *testcontainers.DockerNetwork, pg *tcpostgres.PostgresContainer) string {
	t.Helper()
	config := `[server]
addr = ":8080"
metrics_addr = ":9090"
log_level = "debug"

[queue]
max_attempts = 2

[sources.dmhy]
interval = "1h"
timeout = "30s"
url = "http://dmhy"
category = "2"
max_rps = 0
cache_ttl = "0s"
`
	c, err := testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:        repoRoot(t),
			Dockerfile:     "Dockerfile",
			Repo:           "indexer-e2e-service",
			Tag:            "latest",
			BuildLogWriter: os.Stderr,
		}),
		tcnetwork.WithNetwork([]string{"indexer"}, nw),
		testcontainers.WithEnv(map[string]string{
			"KURA_RELEASES_DATABASE_URL": "postgres://kura:kura@postgres:5432/kura?sslmode=disable",
		}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            strings.NewReader(config),
			ContainerFilePath: "/etc/kura/release-indexer.toml",
			FileMode:          0o644,
		}),
		testcontainers.WithExposedPorts("8080/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/healthz").WithPort("8080/tcp").WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		t.Fatalf("start indexer container after postgres %s: %v", pg.MustConnectionString(ctx, "sslmode=disable"), err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer stopCancel()
		_ = c.Terminate(stopCtx)
	})
	endpoint, err := c.Endpoint(ctx, "http")
	if err != nil {
		t.Fatalf("indexer endpoint: %v", err)
	}
	return endpoint
}

func waitForAvailable(t *testing.T, indexerURL string, want int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if stats := queueStats(t, indexerURL); stats.Available == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("queue did not reach %d available releases", want)
}

func claimOne(t *testing.T, indexerURL, wantInfohash string, leaseSeconds int) int64 {
	t.Helper()
	var claim claimResponse
	postJSON(t, indexerURL+"/api/v1/releases/queue/claim", map[string]any{
		"limit":        1,
		"leaseSeconds": leaseSeconds,
	}, http.StatusOK, &claim)
	if len(claim.Items) != 1 {
		t.Fatalf("claim items = %+v, want one item %s", claim.Items, wantInfohash)
	}
	item := claim.Items[0]
	if item.Infohash != wantInfohash {
		t.Fatalf("claimed infohash = %s, want %s", item.Infohash, wantInfohash)
	}
	if item.ClaimToken == 0 {
		t.Fatalf("claimed item = %+v, want token", item)
	}
	if len(item.RawItems) != 1 || item.RawItems[0].Source != api.SourceDMHY || item.RawItems[0].Title == "" {
		t.Fatalf("claimed raw items = %+v, want crawled DMHY evidence", item.RawItems)
	}
	return item.ClaimToken
}

func assertNoClaim(t *testing.T, indexerURL, note string) {
	t.Helper()
	var claim claimResponse
	postJSON(t, indexerURL+"/api/v1/releases/queue/claim", map[string]any{
		"limit":        1,
		"leaseSeconds": 1,
	}, http.StatusOK, &claim)
	if len(claim.Items) != 0 {
		t.Fatalf("claim during %s = %+v, want no items", note, claim.Items)
	}
}

func submitOK(t *testing.T, indexerURL string, body map[string]any) {
	t.Helper()
	assertSubmitStatus(t, indexerURL, http.StatusOK, body)
}

func assertSubmitStatus(t *testing.T, indexerURL string, wantStatus int, body map[string]any) {
	t.Helper()
	var out map[string]any
	postJSON(t, indexerURL+"/api/v1/releases/queue/submit", body, wantStatus, &out)
}

func queueStats(t *testing.T, indexerURL string) queueStatsResponse {
	t.Helper()
	resp, err := http.Get(indexerURL + "/api/v1/releases/queue/stats")
	if err != nil {
		t.Fatalf("GET /queue/stats: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /queue/stats = %d, want 200", resp.StatusCode)
	}
	var stats queueStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode /queue/stats: %v", err)
	}
	return stats
}

// assertConsumerAPI checks the read surface after the full workflow: the
// matched release is listable under its ref, lists stay magnet-free, and the
// magnet is retrievable per-release.
//
// This ran through the leaf MCP server until that was removed; the assertions
// are unchanged, only the transport. The MCP surface itself is now the
// gateway's and is covered by its own suite.
func assertConsumerAPI(t *testing.T, ctx context.Context, indexerURL, wantMagnet string) {
	t.Helper()

	var list listReleasesResponse
	getJSON(t, ctx, indexerURL+"/api/v1/releases?ref=tvdb:12345&limit=10", http.StatusOK, &list)
	if len(list.Releases) != 1 {
		t.Fatalf("list returned %d releases, want 1", len(list.Releases))
	}
	release := list.Releases[0]
	if release.Infohash != matchInfohash || release.Confidence != 0 {
		t.Fatalf("list item = %+v, want matched infohash with zero confidence", release)
	}
	if _, ok := release.Raw["magnet"]; ok {
		t.Fatalf("list leaked magnet field: %+v", release.Raw)
	}
	if release.Raw["ref"] != "tvdb:12345" {
		t.Fatalf("list ref = %v, want tvdb:12345", release.Raw["ref"])
	}

	var empty listReleasesResponse
	getJSON(t, ctx, indexerURL+"/api/v1/releases?ref=tvdb:99999&limit=10", http.StatusOK, &empty)
	if len(empty.Releases) != 0 {
		t.Fatalf("list for an unmatched ref = %+v, want none", empty.Releases)
	}

	var magnet magnetResponse
	getJSON(t, ctx, indexerURL+"/api/v1/releases/"+matchInfohash+"/magnet", http.StatusOK, &magnet)
	if magnet.Magnet != wantMagnet {
		t.Fatalf("magnet = %q, want %q", magnet.Magnet, wantMagnet)
	}
	// An unknown infohash is a 404, not an empty body.
	getJSON(t, ctx, indexerURL+"/api/v1/releases/"+unknownInfohash+"/magnet", http.StatusNotFound, nil)
}

func getJSON(t *testing.T, ctx context.Context, url string, wantStatus int, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s = %d, want %d; body %s", url, resp.StatusCode, wantStatus, body)
	}
	if out == nil {
		return
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("decode %s response %q: %v", url, body, err)
	}
}

func postJSON(t *testing.T, url string, body any, wantStatus int, out any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s: %v", url, err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(resp.Body)
		t.Fatalf("POST %s = %d, want %d; body=%s", url, resp.StatusCode, wantStatus, raw.String())
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, ".."))
	// go.work moved to the monorepo root; go.mod marks the service root,
	// which is what the docker build contexts here need.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve service root from %s: %v", wd, err)
	}
	return root
}

func fakeDMHYContext(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), `FROM nginx:1.29-alpine
COPY topics /usr/share/nginx/html/topics
`)
	writeFile(t, filepath.Join(dir, "topics", "list", "sort_id", "2", "page", "1"), fakeArchivePage(
		row("1001", "E2E Match Release", matchInfohash, "1.5GB", "2026/06/24 22:25", "udp://tracker.match/announce"),
		row("1002", "E2E Suppress Release", suppressInfohash, "700MB", "2026/06/24 22:24", "udp://tracker.suppress/announce"),
		row("1003", "E2E Exhaust Release", exhaustInfohash, "350MB", "2026/06/24 22:23", "udp://tracker.exhaust/announce"),
	))
	empty := fakeArchivePage()
	writeFile(t, filepath.Join(dir, "topics", "list", "sort_id", "2", "page", "2"), empty)
	writeFile(t, filepath.Join(dir, "topics", "list", "sort_id", "2", "page", "3"), empty)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func fakeArchivePage(rows ...string) string {
	return `<!doctype html><html><body><table id="topic_list"><tbody>` + strings.Join(rows, "\n") + `</tbody></table></body></html>`
}

func row(sourceID, title, infohash, size, published, tracker string) string {
	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&tr=%s&tr=http://tracker.common/announce", infohash, tracker)
	bare := fmt.Sprintf("magnet:?xt=urn:btih:%s", infohash)
	return fmt.Sprintf(`<tr class="">
<td class="title"><a href="/topics/view/%s_e2e.html">%s</a></td>
<td><a class="arrow-magnet" href="%s">download</a><a data-magnet="%s"></a></td>
<td align="center">%s</td>
<td><span style="display: none;">%s</span></td>
</tr>`, sourceID, title, magnet, bare, size, published)
}

type claimResponse struct {
	Items []claimItem `json:"items"`
}

type claimItem struct {
	Infohash     string    `json:"infohash"`
	ClaimToken   int64     `json:"claimToken"`
	AttemptCount int       `json:"attemptCount"`
	RawItems     []rawItem `json:"rawItems"`
}

type rawItem struct {
	Source string `json:"source"`
	Title  string `json:"title"`
}

type queueStatsResponse struct {
	Available  int `json:"available"`
	Leased     int `json:"leased"`
	Unmatched  int `json:"unmatched"`
	Matched    int `json:"matched"`
	Suppressed int `json:"suppressed"`
	Exhausted  int `json:"exhausted"`
}

type listReleasesResponse struct {
	Releases []releaseItem `json:"items"`
}

type releaseItem struct {
	Infohash   string         `json:"infohash"`
	Confidence float64        `json:"confidence"`
	Raw        map[string]any `json:"-"`
}

func (r *releaseItem) UnmarshalJSON(b []byte) error {
	type alias releaseItem
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*r = releaseItem(a)
	r.Raw = raw
	return nil
}

type magnetResponse struct {
	Infohash string `json:"infohash"`
	Magnet   string `json:"magnet"`
}
