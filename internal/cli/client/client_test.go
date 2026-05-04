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
	err := c.Do(context.Background(), http.MethodGet, "/foo", nil, nil, nil, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsKind(err, "invalid_ref") {
		t.Errorf("kind: got %v want invalid_ref", err)
	}
}

func TestClient_OperatorHeader(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get(headerOperator)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL).AsOperator()
	if err := c.Do(context.Background(), http.MethodPost, "/foo", nil, nil, nil, false); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seen != "1" {
		t.Errorf("X-Kura-Operator: got %q want 1", seen)
	}
}

func TestClient_DiscoveryHint(t *testing.T) {
	c := New("http://127.0.0.1:1") // unlikely to be listening
	err := c.Do(context.Background(), http.MethodGet, "/foo", nil, nil, nil, false)
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
