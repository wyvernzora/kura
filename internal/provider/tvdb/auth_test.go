package tvdb

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/textnorm"
)

func TestClientRefreshesTokenAfterUnauthorized(t *testing.T) {
	unauthorizedOnce := true
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/search" && unauthorizedOnce {
			unauthorizedOnce = false
			http.Error(w, "expired", http.StatusUnauthorized)
			return true
		}
		return false
	})
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := p.Search(context.Background(), textnorm.NFC("honzuki"), provider.SearchOptions{}); err != nil {
		t.Fatalf("Search after token refresh: %v", err)
	}
}

func TestNewUsesBoundedDefaultHTTPClient(t *testing.T) {
	p, err := New("test-key", Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if p.client.httpClient.Timeout != defaultHTTPTimeout {
		t.Fatalf("default HTTP timeout = %s, want %s", p.client.httpClient.Timeout, defaultHTTPTimeout)
	}
}

func TestTokenLoginIsSingleflight(t *testing.T) {
	var mu sync.Mutex
	loginCalls := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		loginCalls++
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"token": "token",
			},
		})
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data":   []map[string]any{},
		})
	})
	server := newLocalHTTPServer(t, mux)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := p.Search(context.Background(), textnorm.NFC("honzuki"), provider.SearchOptions{})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if loginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls)
	}
}
