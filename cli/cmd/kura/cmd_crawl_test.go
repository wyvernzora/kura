package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/cli/internal/cli/client"
	relapi "github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

func TestCrawlThreadsCursorUntilDone(t *testing.T) {
	var requests []relapi.CrawlRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req relapi.CrawlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests = append(requests, req)
		result := relapi.CrawlResult{
			Source:     relapi.SourceDMHY,
			Posts:      100,
			Batch:      relapi.IngestBatch{New: 100},
			NextCursor: "second",
			HasMore:    true,
			StopReason: relapi.CrawlStopPageBudget,
		}
		if req.Cursor == "second" {
			result.Posts = 40
			result.Batch.New = 40
			result.NextCursor = ""
			result.HasMore = false
			result.StopReason = relapi.CrawlStopLookbackBoundary
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := run([]string{"crawl", "dmhy", "5w", "--json"}, runContext{
		Stdout: &out,
		Getenv: func(name string) string {
			if name == client.EnvBaseURL {
				return srv.URL
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if requests[0].Cursor != "" || requests[1].Cursor != "second" {
		t.Fatalf("cursors = %q, %q", requests[0].Cursor, requests[1].Cursor)
	}
	for i, req := range requests {
		if req.PageSize != 200 || req.Lookback != "5w" {
			t.Fatalf("request %d = %+v", i, req)
		}
	}

	dec := json.NewDecoder(&out)
	var first, second relapi.CrawlResult
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("decode first result: %v", err)
	}
	if err := dec.Decode(&second); err != nil {
		t.Fatalf("decode second result: %v", err)
	}
	if first.Posts != 100 || second.Posts != 40 || second.HasMore {
		t.Fatalf("results = %+v, %+v", first, second)
	}
}

func TestCrawlStartsFromResumeCursor(t *testing.T) {
	var got relapi.CrawlRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(relapi.CrawlResult{
			Source:     relapi.SourceDMHY,
			StopReason: relapi.CrawlStopLookbackBoundary,
		})
	}))
	defer srv.Close()

	err := run([]string{"crawl", "dmhy", "30d", "--cursor", "resume-here"}, runContext{
		Stdout: &bytes.Buffer{},
		Getenv: func(name string) string {
			if name == client.EnvBaseURL {
				return srv.URL
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got.Cursor != "resume-here" || got.Lookback != "30d" || got.PageSize != 200 {
		t.Fatalf("request = %+v", got)
	}
}

func TestCrawlErrorPrintsResumeCommand(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(relapi.CrawlResult{
				Source:     relapi.SourceDMHY,
				Posts:      100,
				NextCursor: "resume-here",
				HasMore:    true,
				StopReason: relapi.CrawlStopPageBudget,
			})
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(relapi.Error{
			Kind:    relapi.KindUpstreamError,
			Message: "page fetch failed",
		})
	}))
	defer srv.Close()

	err := run([]string{"crawl", "dmhy", "30d"}, runContext{
		Stdout: &bytes.Buffer{},
		Getenv: func(name string) string {
			if name == client.EnvBaseURL {
				return srv.URL
			}
			return ""
		},
	})
	if err == nil {
		t.Fatal("expected second chunk to fail")
	}
	want := "kura crawl dmhy 30d --cursor resume-here"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want resume command %q", err, want)
	}
}

func TestCrawlHumanOutputIncludesCheckpoint(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		result := relapi.CrawlResult{
			Source:     relapi.SourceDMHY,
			Posts:      200,
			NextCursor: "checkpoint-token",
			HasMore:    true,
			StopReason: relapi.CrawlStopPageBudget,
		}
		if calls == 2 {
			result.Posts = 0
			result.NextCursor = ""
			result.HasMore = false
			result.StopReason = relapi.CrawlStopLookbackBoundary
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := run([]string{"crawl", "dmhy", "30d"}, runContext{
		Stdout: &out,
		Getenv: func(name string) string {
			if name == client.EnvBaseURL {
				return srv.URL
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(out.String(), "checkpoint checkpoint-token") {
		t.Fatalf("stdout = %q, want checkpoint", out.String())
	}
}
