package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/gateway/internal/client"
)

// recorded is one upstream request the bridge made.
type recorded struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]any
}

// recorder is a stub leaf that records what it was asked and replies with a
// canned body.
type recorder struct {
	mu     sync.Mutex
	got    []recorded
	status int
	body   string
}

func (r *recorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		entry := recorded{Method: req.Method, Path: req.URL.Path, Query: req.URL.Query()}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &entry.Body)
		}
		r.mu.Lock()
		r.got = append(r.got, entry)
		status, body := r.status, r.body
		r.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if body == "" {
			body = "{}"
		}
		_, _ = io.WriteString(w, body)
	}
}

func (r *recorder) calls() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.got...)
}

func (r *recorder) reply(status int, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status, r.body = status, body
	r.got = nil
}

// harness wires the bridge to two recording leaves.
type harness struct {
	sess     *mcpsdk.ClientSession
	library  *recorder
	releases *recorder
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	lib, rel := &recorder{}, &recorder{}
	libSrv := httptest.NewServer(lib.handler())
	relSrv := httptest.NewServer(rel.handler())
	t.Cleanup(libSrv.Close)
	t.Cleanup(relSrv.Close)

	opts := client.Options{RequestTimeout: 5 * time.Second, MaxResponseBytes: 1 << 20}
	s := New("test",
		client.New(strings.TrimPrefix(libSrv.URL, "http://"), opts),
		client.New(strings.TrimPrefix(relSrv.URL, "http://"), opts), nil)

	ct, st := mcpsdk.NewInMemoryTransports()
	go func() { _, _ = s.sdk.Connect(context.Background(), st, nil) }()
	sess, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return &harness{sess: sess, library: lib, releases: rel}
}

func (h *harness) call(t *testing.T, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := h.sess.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	return res
}

// Every tool must reach the documented REST method and path, and release tools
// must never touch the library upstream (or vice versa).
func TestToolsCallTheirDocumentedRestEndpoint(t *testing.T) {
	for _, tc := range []struct {
		tool       string
		args       map[string]any
		onReleases bool
		method     string
		path       string
	}{
		{"resolve_series", map[string]any{"terms": []string{"bocchi"}}, false, "POST", "/api/v1/series/resolve"},
		{"list_series", map[string]any{}, false, "GET", "/api/v1/series"},
		{"get_series", map[string]any{"ref": "tvdb:42"}, false, "GET", "/api/v1/series/tvdb:42"},
		{"update_series_tags", map[string]any{"ref": "tvdb:42", "tags": []string{"x"}}, false, "PATCH", "/api/v1/series/tvdb:42/tags"},
		{"add_series", map[string]any{"ref": "tvdb:42"}, false, "POST", "/api/v1/series"},
		{"import_series", map[string]any{"ref": "tvdb:42", "directory": "Show"}, false, "POST", "/api/v1/series/import"},
		{"scan_series", map[string]any{"ref": "tvdb:42"}, false, "POST", "/api/v1/series/tvdb:42/scan"},
		{"stage_series_media", map[string]any{"ref": "tvdb:42"}, false, "POST", "/api/v1/series/tvdb:42/stage"},
		{"reset_series_staging", map[string]any{"ref": "tvdb:42", "all": true}, false, "POST", "/api/v1/series/tvdb:42/reset"},
		{"plan_series_reconcile", map[string]any{"ref": "tvdb:42"}, false, "POST", "/api/v1/series/tvdb:42/reconcile/plan"},
		{"apply_series_reconcile", map[string]any{"ref": "tvdb:42", "token": "t"}, false, "POST", "/api/v1/series/tvdb:42/reconcile/apply"},
		{"get_job", map[string]any{"jobId": "01J"}, false, "GET", "/api/v1/jobs/01J"},
		{"list_inbox", map[string]any{}, false, "GET", "/api/v1/inbox"},
		{"list_releases", map[string]any{}, true, "GET", "/api/v1/releases"},
		{"get_release", map[string]any{"infohash": "abc"}, true, "GET", "/api/v1/releases/abc"},
		{"get_magnet", map[string]any{"infohash": "abc"}, true, "GET", "/api/v1/releases/abc/magnet"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			h := newHarness(t)
			h.call(t, tc.tool, tc.args)

			hit, idle := h.library, h.releases
			if tc.onReleases {
				hit, idle = h.releases, h.library
			}
			calls := hit.calls()
			if len(calls) != 1 {
				t.Fatalf("expected 1 upstream call, got %d: %+v", len(calls), calls)
			}
			if calls[0].Method != tc.method || calls[0].Path != tc.path {
				t.Errorf("called %s %s, want %s %s", calls[0].Method, calls[0].Path, tc.method, tc.path)
			}
			if other := idle.calls(); len(other) != 0 {
				t.Errorf("touched the wrong upstream: %+v", other)
			}
		})
	}
}

// Repeated values must become repeated query parameters, not a joined string.
func TestQueryArrayTranslation(t *testing.T) {
	h := newHarness(t)
	h.call(t, "list_series", map[string]any{
		"status": []string{"complete", "incomplete"},
		"tags":   []string{"priority:high", "maintenance:requested"},
		"airing": true,
	})
	calls := h.library.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	q := calls[0].Query
	if got := q["status"]; len(got) != 2 || got[0] != "complete" || got[1] != "incomplete" {
		t.Errorf("status = %v, want two repeated params", got)
	}
	if got := q["tags"]; len(got) != 2 {
		t.Errorf("tags = %v, want two repeated params", got)
	}
	if got := q.Get("airing"); got != "true" {
		t.Errorf("airing = %q, want true", got)
	}
}

// Over-large limits clamp rather than being rejected — the caps are handler
// behaviour, deliberately not schema maxima.
func TestListLimitsClampRatherThanReject(t *testing.T) {
	for _, tc := range []struct {
		tool       string
		onReleases bool
		want       string
	}{
		{"list_series", false, "100"},
		{"list_releases", true, "100"},
		{"list_inbox", false, "500"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			h := newHarness(t)
			res := h.call(t, tc.tool, map[string]any{"limit": 99999})
			if res.IsError {
				t.Fatalf("over-large limit rejected: %s", firstText(res))
			}
			hit := h.library
			if tc.onReleases {
				hit = h.releases
			}
			if got := hit.calls()[0].Query.Get("limit"); got != tc.want {
				t.Errorf("limit = %q, want clamped to %q", got, tc.want)
			}
		})
	}
}

// A mutation that fails must be attempted exactly once: a retried stage or
// apply is a second filesystem effect.
func TestMutationsAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"add_series", map[string]any{"ref": "tvdb:42"}},
		{"scan_series", map[string]any{"ref": "tvdb:42"}},
		{"stage_series_media", map[string]any{"ref": "tvdb:42"}},
		{"apply_series_reconcile", map[string]any{"ref": "tvdb:42", "token": "t"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			h := newHarness(t)
			h.library.reply(http.StatusInternalServerError, `{"kind":"internal","message":"boom"}`)
			res := h.call(t, tc.tool, tc.args)
			if !res.IsError {
				t.Fatal("expected an error result")
			}
			if n := len(h.library.calls()); n != 1 {
				t.Errorf("upstream attempts = %d, want exactly 1", n)
			}
		})
	}
}

func TestAsyncToolsReturnOnlyJobID(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"scan_series", map[string]any{"ref": "tvdb:42"}},
		{"stage_series_media", map[string]any{"ref": "tvdb:42"}},
		{"apply_series_reconcile", map[string]any{"ref": "tvdb:42", "token": "t"}},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			h := newHarness(t)
			h.library.reply(http.StatusAccepted,
				`{"jobId":"01JOB","kind":"scan","statusUrl":"/api/v1/jobs/01JOB","streamUrl":"/api/v1/jobs/01JOB/stream","submittedAt":"2026-01-01T00:00:00Z"}`)
			res := h.call(t, tc.tool, tc.args)
			if res.IsError {
				t.Fatalf("unexpected error: %s", firstText(res))
			}
			got, ok := res.StructuredContent.(map[string]any)
			if !ok {
				t.Fatalf("structuredContent = %#v", res.StructuredContent)
			}
			if got["jobId"] != "01JOB" {
				t.Errorf("jobId = %v", got["jobId"])
			}
			if len(got) != 1 {
				t.Errorf("returned %v, want only jobId — REST urls are not MCP affordances", got)
			}
		})
	}
}

func TestGetJobResultRules(t *testing.T) {
	t.Run("succeeded omits result", func(t *testing.T) {
		h := newHarness(t)
		h.library.reply(http.StatusOK, `{"jobId":"01J","kind":"scan","state":"succeeded",
			"progress":{"phase":"done","status":"ok","current":3,"total":3},
			"result":{"synced":[{"episode":"S01E01"}]}}`)
		got := structured(t, h.call(t, "get_job", map[string]any{"jobId": "01J"}))
		if _, present := got["result"]; present {
			t.Error("result present on a succeeded job; the agent reads the outcome via get_series")
		}
		for _, key := range []string{"state", "progress"} {
			if _, present := got[key]; !present {
				t.Errorf("%s missing", key)
			}
		}
	})

	t.Run("failed keeps result", func(t *testing.T) {
		h := newHarness(t)
		h.library.reply(http.StatusOK, `{"jobId":"01J","kind":"scan","state":"failed",
			"progress":{"phase":"scan","status":"error","current":1,"total":3},
			"error":{"kind":"internal","message":"disk"},
			"result":{"synced":[{"episode":"S01E01"}]}}`)
		got := structured(t, h.call(t, "get_job", map[string]any{"jobId": "01J"}))
		if _, present := got["result"]; !present {
			t.Error("result missing on a failed job; the agent needs the partial outcome to retry")
		}
		for _, key := range []string{"state", "progress", "error"} {
			if _, present := got[key]; !present {
				t.Errorf("%s missing", key)
			}
		}
	})

	t.Run("failed result is bounded", func(t *testing.T) {
		h := newHarness(t)
		big := strings.Repeat("x", responseBudget+1024)
		h.library.reply(http.StatusOK,
			`{"jobId":"01J","kind":"scan","state":"failed","result":{"blob":"`+big+`"}}`)
		got := structured(t, h.call(t, "get_job", map[string]any{"jobId": "01J"}))
		if got["resultTruncated"] != true {
			t.Errorf("resultTruncated = %v, want true", got["resultTruncated"])
		}
		marker, ok := got["result"].(map[string]any)
		if !ok || marker["omitted"] == nil {
			t.Errorf("result = %#v, want the omission marker rather than a cut document", got["result"])
		}
	})
}

// get_series drops host-side fields and refuses deterministically when the
// projection would exceed the budget.
func TestGetSeriesProjection(t *testing.T) {
	h := newHarness(t)
	h.library.reply(http.StatusOK,
		`{"ref":"tvdb:42","directory":"Show","root":"library:Show","generation":7,"preferredTitle":"Show","seasons":[]}`)
	got := structured(t, h.call(t, "get_series", map[string]any{"ref": "tvdb:42"}))
	for _, hostSide := range []string{"directory", "root", "generation"} {
		if _, present := got[hostSide]; present {
			t.Errorf("%s leaked into the agent projection", hostSide)
		}
	}
	if got["ref"] != "tvdb:42" || got["preferredTitle"] != "Show" {
		t.Errorf("projection dropped agent-facing fields: %v", got)
	}

	h2 := newHarness(t)
	h2.library.reply(http.StatusOK,
		`{"ref":"tvdb:42","preferredTitle":"`+strings.Repeat("y", responseBudget)+`"}`)
	res := h2.call(t, "get_series", map[string]any{"ref": "tvdb:42"})
	if !res.IsError {
		t.Fatal("oversized projection succeeded, want a deterministic refusal")
	}
	var env client.Error
	if err := json.Unmarshal([]byte(firstText(res)), &env); err != nil {
		t.Fatalf("error content: %v", err)
	}
	if env.Kind != "response_too_large" {
		t.Errorf("kind = %q, want response_too_large", env.Kind)
	}
}

// Request bodies must match the documented REST shape, including the rename
// of dirname to directory.
func TestRequestBodiesMatchRestShape(t *testing.T) {
	h := newHarness(t)
	h.call(t, "import_series", map[string]any{"ref": "tvdb:42", "directory": "Show", "ordering": "aired"})
	body := h.library.calls()[0].Body
	if body["ref"] != "tvdb:42" || body["directory"] != "Show" || body["ordering"] != "aired" {
		t.Errorf("body = %v", body)
	}
	// force overwrites a tracked series' metadata and stays REST-only.
	if _, present := body["force"]; present {
		t.Error("force reached the wire; it is deliberately not on this surface")
	}
}

func TestStageAcceptsObjectEntriesAndForwardsTheirShape(t *testing.T) {
	h := newHarness(t)
	h.call(t, "stage_series_media", map[string]any{
		"ref": "tvdb:42",
		"episodes": []any{
			map[string]any{
				"episode": "S01E01",
				"media":   "inbox:episode.mkv",
				"source":  "WebRip",
			},
		},
	})

	episodes, ok := h.library.calls()[0].Body["episodes"].([]any)
	if !ok || len(episodes) != 1 {
		t.Fatalf("episodes = %#v, want one object entry", h.library.calls()[0].Body["episodes"])
	}
	episode, ok := episodes[0].(map[string]any)
	if !ok {
		t.Fatalf("episode = %#v, want object", episodes[0])
	}
	if episode["episode"] != "S01E01" ||
		episode["media"] != "inbox:episode.mkv" ||
		episode["source"] != "WebRip" {
		t.Fatalf("episode = %#v, want REST staging shape", episode)
	}
}

func TestEveryToolHasTypedSchemas(t *testing.T) {
	h := newHarness(t)
	res, err := h.sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.InputSchema == nil {
			t.Errorf("%s: no input schema", tool.Name)
		}
		if tool.OutputSchema == nil {
			t.Errorf("%s: no output schema", tool.Name)
		}
	}
}

func structured(t *testing.T, res *mcpsdk.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool returned an error: %s", firstText(res))
	}
	got, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent = %#v", res.StructuredContent)
	}
	return got
}
