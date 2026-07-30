//go:build e2e

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	productRef      = "tvdb:12345"
	productInfohash = "0123456789abcdef0123456789abcdef01234567"
	productMagnet   = "magnet:?xt=urn:btih:" + productInfohash + "&tr=udp://tracker.product/announce"
	productDir      = "Product Series"
	inboxMedia      = "Product.Series.S01E01.1080p.mkv"
	canonicalMedia  = "Product Series - S01E01 (WebRip 1080p).mkv"
)

func TestProduct(t *testing.T) {
	buildProductImages(t)

	t.Run("release to library", testReleaseToLibrary)
	t.Run("restart durability", testRestartDurability)
	t.Run("partial outage recovery", testPartialOutageRecovery)
	t.Run("streaming and shutdown", testStreamingAndShutdown)
	t.Run("MCP through Kubernetes Service authority", testMCPServiceAuthority)
	t.Run("n8n action workflow", testN8NActionWorkflow)
	t.Run("n8n polling trigger workflow", testN8NTriggerWorkflow)
}

func testReleaseToLibrary(t *testing.T) {
	stack := startProductStack(t)
	session := connectMCP(t, stack.gatewayURL)

	addSeries(t, session)
	matchRelease(t, stack)
	assertReleaseTools(t, session)

	copyMediaFixture(t, stack)
	stageJob := callJobTool(t, session, "stage_series_media", map[string]any{
		"ref": productRef,
		"episodes": []any{
			map[string]any{
				"episode": "S01E01",
				"media":   "inbox:" + inboxMedia,
				"source":  "WebRip",
			},
		},
	})
	waitJob(t, session, stageJob)

	plan := callTool(t, session, "plan_series_reconcile", map[string]any{"ref": productRef})
	token := stringField(t, plan, "token")
	applyJob := callJobTool(t, session, "apply_series_reconcile", map[string]any{
		"ref":   productRef,
		"token": token,
	})
	waitJob(t, session, applyJob)

	series := getSeries(t, session)
	episode := onlyEpisode(t, series)
	if episode.Status != "present" {
		t.Fatalf("episode status = %q, want present", episode.Status)
	}
	if episode.Active == nil {
		t.Fatal("episode has no active media after reconcile")
	}
	if episode.Active.File != "series:Season 1/"+canonicalMedia {
		t.Fatalf("active file = %q, want canonical selector", episode.Active.File)
	}
	if episode.Active.Source != "WebRip" || episode.Active.Resolution != "1080p" {
		t.Fatalf("active media = %+v, want WebRip 1080p", episode.Active)
	}

	canonicalPath := filepath.Join(stack.libraryRoot, productDir, "Season 1", canonicalMedia)
	if _, err := os.Stat(canonicalPath); err != nil {
		t.Fatalf("canonical media missing at %s: %v", canonicalPath, err)
	}
	if _, err := os.Stat(filepath.Join(stack.inboxRoot, inboxMedia)); !os.IsNotExist(err) {
		t.Fatalf("inbox media was not consumed: %v", err)
	}
}

func testRestartDurability(t *testing.T) {
	stack := startProductStack(t)
	session := connectMCP(t, stack.gatewayURL)
	addSeries(t, session)
	matchRelease(t, stack)

	stopTimeout := 10 * time.Second
	if err := stack.library.Stop(stack.ctx, &stopTimeout); err != nil {
		t.Fatalf("stop library-manager: %v", err)
	}
	if err := stack.release.Stop(stack.ctx, &stopTimeout); err != nil {
		t.Fatalf("stop release-indexer: %v", err)
	}
	stack.waitSystemStatus(t, http.StatusServiceUnavailable, "degraded", 15*time.Second)

	if err := stack.library.Start(stack.ctx); err != nil {
		t.Fatalf("restart library-manager: %v", err)
	}
	if err := stack.release.Start(stack.ctx); err != nil {
		t.Fatalf("restart release-indexer: %v", err)
	}
	stack.waitSystemStatus(t, http.StatusOK, "ok", 30*time.Second)

	series := getSeries(t, session)
	if series.Ref != productRef {
		t.Fatalf("series ref after restart = %q, want %q", series.Ref, productRef)
	}
	release := callTool(t, session, "get_release", map[string]any{"infohash": productInfohash})
	if stringField(t, release, "matchStatus") != "matched" {
		t.Fatalf("release after restart = %#v, want matched", release)
	}
	if stringField(t, release, "ref") != productRef {
		t.Fatalf("release ref after restart = %#v, want %s", release, productRef)
	}
	if _, err := os.Stat(filepath.Join(stack.libraryRoot, productDir, ".kura", "series.json")); err != nil {
		t.Fatalf("series metadata did not survive restart: %v", err)
	}
}

func testPartialOutageRecovery(t *testing.T) {
	stack := startProductStack(t)
	session := connectMCP(t, stack.gatewayURL)
	addSeries(t, session)

	stopTimeout := 10 * time.Second
	if err := stack.release.Stop(stack.ctx, &stopTimeout); err != nil {
		t.Fatalf("stop release-indexer: %v", err)
	}
	stack.waitSystemStatus(t, http.StatusServiceUnavailable, "degraded", 15*time.Second)
	assertHTTPStatus(t, stack.gatewayURL+"/healthz", http.StatusOK)

	list := callTool(t, session, "list_series", map[string]any{"limit": 10})
	items, ok := list["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("library tool during release outage = %#v, want one series", list)
	}
	assertToolErrorKind(t, session, "list_releases", map[string]any{"limit": 10}, "backend_unavailable")

	if err := stack.release.Start(stack.ctx); err != nil {
		t.Fatalf("restart release-indexer: %v", err)
	}
	stack.waitSystemStatus(t, http.StatusOK, "ok", 30*time.Second)
	callTool(t, session, "list_releases", map[string]any{"limit": 10})
}

func testStreamingAndShutdown(t *testing.T) {
	stack := startProductStack(t)
	session := connectMCP(t, stack.gatewayURL)
	addSeries(t, session)
	copyMediaFixture(t, stack)

	jobID := callJobTool(t, session, "stage_series_media", map[string]any{
		"ref": productRef,
		"episodes": []any{
			map[string]any{
				"episode": "S01E01",
				"media":   "inbox:" + inboxMedia,
				"source":  "WebRip",
			},
		},
	})
	assertJobStream(t, stack.gatewayURL, jobID)
	waitJob(t, session, jobID)

	tools, err := session.ListTools(callContext(t), nil)
	if err != nil {
		t.Fatalf("list tools before shutdown: %v", err)
	}
	if len(tools.Tools) != 16 {
		t.Fatalf("tool count before shutdown = %d, want 16", len(tools.Tools))
	}

	started := time.Now()
	stopTimeout := 15 * time.Second
	if err := stack.gateway.Stop(stack.ctx, &stopTimeout); err != nil {
		t.Fatalf("stop gateway with MCP SSE session open: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= stopTimeout {
		t.Fatalf("gateway drain took %s, want less than %s", elapsed, stopTimeout)
	}

	if err := stack.gateway.Start(stack.ctx); err != nil {
		t.Fatalf("restart gateway: %v", err)
	}
	stack.refreshGatewayURL(t)
	stack.waitSystemStatus(t, http.StatusOK, "ok", 30*time.Second)
	reconnected := connectMCP(t, stack.gatewayURL)
	tools, err = reconnected.ListTools(callContext(t), nil)
	if err != nil {
		t.Fatalf("list tools after gateway restart: %v", err)
	}
	if len(tools.Tools) != 16 {
		t.Fatalf("tool count after restart = %d, want 16", len(tools.Tools))
	}
}

func testMCPServiceAuthority(t *testing.T) {
	stack := startProductStack(t)
	session := connectMCPWithHost(t, stack.gatewayURL, "kura.kura.svc.cluster.local")
	tools, err := session.ListTools(callContext(t), nil)
	if err != nil {
		t.Fatalf("list tools through Kubernetes Service authority: %v", err)
	}
	if len(tools.Tools) != 16 {
		t.Fatalf("tool count through Kubernetes Service authority = %d, want 16", len(tools.Tools))
	}
}

func connectMCP(t *testing.T, gatewayURL string) *mcpsdk.ClientSession {
	t.Helper()
	return connectMCPWithHost(t, gatewayURL, "")
}

func connectMCPWithHost(t *testing.T, gatewayURL, host string) *mcpsdk.ClientSession {
	t.Helper()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "kura-product-e2e",
		Version: testVersion,
	}, nil)
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:   gatewayURL + "/mcp/v1",
		MaxRetries: -1,
	}
	if host != "" {
		transport.HTTPClient = &http.Client{
			Transport: rewriteHostTransport{host: host},
		}
	}
	session, err := client.Connect(callContext(t), transport, nil)
	if err != nil {
		t.Fatalf("connect MCP streamable HTTP: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
	})
	return session
}

type rewriteHostTransport struct {
	host string
}

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Host = t.host
	return http.DefaultTransport.RoundTrip(clone)
}

func callTool(
	t *testing.T,
	session *mcpsdk.ClientSession,
	name string,
	arguments map[string]any,
) map[string]any {
	t.Helper()
	result, err := session.CallTool(callContext(t), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("%s protocol error: %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s tool error: %s", name, toolText(result))
	}
	out, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("%s structured content = %T, want object", name, result.StructuredContent)
	}
	return out
}

func callJobTool(
	t *testing.T,
	session *mcpsdk.ClientSession,
	name string,
	arguments map[string]any,
) string {
	t.Helper()
	return stringField(t, callTool(t, session, name, arguments), "jobId")
}

func waitJob(t *testing.T, session *mcpsdk.ClientSession, jobID string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last map[string]any
	for time.Now().Before(deadline) {
		last = callTool(t, session, "get_job", map[string]any{"jobId": jobID})
		switch stringField(t, last, "state") {
		case "succeeded":
			return
		case "failed":
			t.Fatalf("job %s failed: %#v", jobID, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("job %s did not complete: %#v", jobID, last)
}

func addSeries(t *testing.T, session *mcpsdk.ClientSession) {
	t.Helper()
	added := callTool(t, session, "add_series", map[string]any{
		"ref":       productRef,
		"directory": productDir,
	})
	if stringField(t, added, "ref") != productRef {
		t.Fatalf("added series = %#v, want ref %s", added, productRef)
	}
}

func matchRelease(t *testing.T, stack *productStack) {
	t.Helper()
	waitQueueAvailable(t, stack.gatewayURL)

	var claim struct {
		Items []struct {
			Infohash   string `json:"infohash"`
			ClaimToken int64  `json:"claimToken"`
		} `json:"items"`
	}
	postJSON(t, stack.gatewayURL+"/api/v1/releases/queue/claim", map[string]any{
		"limit":        1,
		"leaseSeconds": 30,
	}, &claim)
	if len(claim.Items) != 1 || claim.Items[0].Infohash != productInfohash || claim.Items[0].ClaimToken == 0 {
		t.Fatalf("release claim = %+v, want product fixture", claim)
	}
	postJSON(t, stack.gatewayURL+"/api/v1/releases/queue/submit", map[string]any{
		"infohash":   productInfohash,
		"claimToken": claim.Items[0].ClaimToken,
		"status":     "matched",
		"ref":        productRef,
		"confidence": 1,
		"reason":     "product e2e fixture",
	}, &map[string]any{})
}

func waitQueueAvailable(t *testing.T, gatewayURL string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last map[string]int
	for time.Now().Before(deadline) {
		getJSON(t, gatewayURL+"/api/v1/releases/queue/stats", &last)
		if last["available"] == 1 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("release queue did not receive crawler fixture: %+v", last)
}

func assertReleaseTools(t *testing.T, session *mcpsdk.ClientSession) {
	t.Helper()
	list := callTool(t, session, "list_releases", map[string]any{
		"ref":   productRef,
		"limit": 10,
	})
	items, ok := list["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("list_releases = %#v, want one matched release", list)
	}
	release := callTool(t, session, "get_release", map[string]any{"infohash": productInfohash})
	if stringField(t, release, "ref") != productRef ||
		stringField(t, release, "matchStatus") != "matched" {
		t.Fatalf("get_release = %#v, want matched %s", release, productRef)
	}
	magnet := callTool(t, session, "get_magnet", map[string]any{"infohash": productInfohash})
	if stringField(t, magnet, "magnet") != productMagnet {
		t.Fatalf("get_magnet = %#v, want stored magnet", magnet)
	}
}

type seriesView struct {
	Ref     string `json:"ref"`
	Seasons []struct {
		Episodes []episodeView `json:"episodes"`
	} `json:"seasons"`
}

type episodeView struct {
	Episode string     `json:"episode"`
	Status  string     `json:"status"`
	Active  *mediaView `json:"active"`
}

type mediaView struct {
	Source     string `json:"source"`
	Resolution string `json:"resolution"`
	File       string `json:"file"`
}

func getSeries(t *testing.T, session *mcpsdk.ClientSession) seriesView {
	t.Helper()
	raw := callTool(t, session, "get_series", map[string]any{"ref": productRef})
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("encode get_series result: %v", err)
	}
	var out seriesView
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("decode get_series result: %v", err)
	}
	return out
}

func onlyEpisode(t *testing.T, series seriesView) episodeView {
	t.Helper()
	var episodes []episodeView
	for _, season := range series.Seasons {
		episodes = append(episodes, season.Episodes...)
	}
	if len(episodes) != 1 {
		t.Fatalf("series episodes = %+v, want one", episodes)
	}
	if episodes[0].Episode != "S01E0001" {
		t.Fatalf("episode = %q, want S01E0001", episodes[0].Episode)
	}
	return episodes[0]
}

func copyMediaFixture(t *testing.T, stack *productStack) {
	t.Helper()
	source := filepath.Join(stack.repoRoot, "e2e", "testdata", "product-episode.mkv")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read media fixture: %v", err)
	}
	target := filepath.Join(stack.inboxRoot, inboxMedia)
	if err := os.WriteFile(target, raw, 0o666); err != nil {
		t.Fatalf("write inbox media: %v", err)
	}
}

func assertJobStream(t *testing.T, gatewayURL, jobID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		gatewayURL+"/api/v1/jobs/"+jobID+"/stream", http.NoBody)
	if err != nil {
		t.Fatalf("build job stream request: %v", err)
	}
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open job stream: %v", err)
	}
	headerElapsed := time.Since(started)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("job stream status = %d: %s", resp.StatusCode, body)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("job stream content type = %q", resp.Header.Get("Content-Type"))
	}
	if headerElapsed >= 2*time.Second {
		t.Fatalf("job stream headers were buffered for %s", headerElapsed)
	}

	scanner := bufio.NewScanner(resp.Body)
	var events []string
	for scanner.Scan() {
		if line := scanner.Text(); strings.HasPrefix(line, "event: ") {
			events = append(events, strings.TrimPrefix(line, "event: "))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read job stream: %v", err)
	}
	if len(events) == 0 || (events[len(events)-1] != "result" && events[len(events)-1] != "error") {
		t.Fatalf("job stream events = %v, want terminal event", events)
	}
}

func assertToolErrorKind(
	t *testing.T,
	session *mcpsdk.ClientSession,
	name string,
	arguments map[string]any,
	wantKind string,
) {
	t.Helper()
	result, err := session.CallTool(callContext(t), &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("%s protocol error: %v", name, err)
	}
	if !result.IsError || result.StructuredContent != nil {
		t.Fatalf("%s result = %#v, want unstructured tool error", name, result)
	}
	var envelope struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(toolText(result)), &envelope); err != nil {
		t.Fatalf("decode %s error envelope: %v", name, err)
	}
	if envelope.Kind != wantKind {
		t.Fatalf("%s error kind = %q, want %q", name, envelope.Kind, wantKind)
	}
}

func toolText(result *mcpsdk.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*mcpsdk.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func stringField(t *testing.T, value map[string]any, field string) string {
	t.Helper()
	out, ok := value[field].(string)
	if !ok || out == "" {
		t.Fatalf("%s = %#v, want non-empty string in %#v", field, value[field], value)
	}
	return out
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	req, err := http.NewRequestWithContext(callContext(t), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("build GET %s: %v", url, err)
	}
	resp, err := productHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode GET %s: %v", url, err)
	}
}

func postJSON(t *testing.T, url string, body, out any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal POST %s: %v", url, err)
	}
	req, err := http.NewRequestWithContext(callContext(t), http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build POST %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := productHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		response, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s = %d: %s", url, resp.StatusCode, response)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode POST %s: %v", url, err)
		}
	}
}

func assertHTTPStatus(t *testing.T, url string, want int) {
	t.Helper()
	req, err := http.NewRequestWithContext(callContext(t), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("build GET %s: %v", url, err)
	}
	resp, err := productHTTPClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET %s = %d, want %d: %s", url, resp.StatusCode, want, body)
	}
}

func callContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}
