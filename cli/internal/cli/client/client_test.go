package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Do_DecodesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"kind":"invalid_ref","category":"invalid_params","message":"bad input"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.Do(context.Background(), http.MethodGet, "/foo", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsKind(err, "invalid_ref") {
		t.Errorf("kind: got %v want invalid_ref", err)
	}
}

// A proxy answering with an HTML error page is the realistic
// non-envelope case; the snippet is what tells the operator that
// something other than kura replied.
func TestClient_Do_NonJSONErrorBodySnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>\n  <body>502 Bad Gateway</body>\n</html>"))
	}))
	defer srv.Close()

	err := New(srv.URL).Do(context.Background(), http.MethodGet, "/foo", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "<html> <body>502 Bad Gateway</body> </html>") {
		t.Errorf("snippet missing: %v", err)
	}
}

func TestBodySnippet(t *testing.T) {
	long := strings.Repeat("a", errBodySnippetMax+10)
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"empty", nil, "(empty)"},
		{"whitespace only", []byte("\n\t  "), "(empty)"},
		{"collapses newlines", []byte("<html>\n  <body>502</body>\n"), "<html> <body>502</body>"},
		{"drops invalid bytes", []byte("bad\xff\xfebody"), "badbody"},
		{"truncates", []byte(long), long[:errBodySnippetMax] + "…"},
		{"cut mid-rune leaves no partial", append([]byte(strings.Repeat("a", errBodySnippetMax-1)), "日"...), strings.Repeat("a", errBodySnippetMax-1) + "…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bodySnippet(tc.body); got != tc.want {
				t.Errorf("bodySnippet = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClient_DiscoveryHint(t *testing.T) {
	c := New("http://127.0.0.1:1") // unlikely to be listening
	err := c.Do(context.Background(), http.MethodGet, "/foo", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "is it running") {
		t.Errorf("hint missing: %v", err)
	}
}

func TestStreamJob_TerminalResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: progress\ndata: {\"phase\":\"x\",\"current\":1,\"total\":2}\n\n"))
		_, _ = w.Write([]byte("event: result\ndata: {\"summary\":\"ok\"}\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	var events []JobEvent
	err := c.StreamJob(context.Background(), "abc", func(ev JobEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("StreamJob: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events: got %d want 2 (%+v)", len(events), events)
	}
	if events[0].Kind != "progress" || events[1].Kind != "result" {
		t.Errorf("kinds: %+v", events)
	}
}

func TestStreamJob_TerminalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: error\ndata: {\"kind\":\"conflict\",\"message\":\"busy\"}\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	err := c.StreamJob(context.Background(), "abc", func(ev JobEvent) {})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsKind(err, "conflict") {
		t.Errorf("kind: got %v", err)
	}
}
