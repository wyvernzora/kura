package tvdb

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestSeriesEpisodesErrorsWhenPaginationExceedsCap(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"token": "token",
			},
		})
	})
	mux.HandleFunc("/series/1/episodes/default", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"episodes": []map[string]any{},
			},
			"links": map[string]any{
				"next": "next-page",
			},
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

	_, err = p.client.seriesEpisodes(context.Background(), "1")
	if !errors.Is(err, metadata.ErrUnavailable) {
		t.Fatalf("seriesEpisodes error = %v, want metadata.ErrUnavailable", err)
	}
}

func TestNormalizeEpisodeRecordNormalizesAiredDate(t *testing.T) {
	got := normalizeEpisodeRecord(episodeRecord{
		ID:           1,
		Aired:        "not-a-date",
		FirstAired:   "2020-01-02",
		Number:       3,
		SeasonNumber: 2,
	}, 2)

	if got.Aired != "2020-01-02" {
		t.Fatalf("Aired = %q, want fallback firstAired date", got.Aired)
	}
}

func TestNormalizeSeasonsUsesEmptyProviderRefForSyntheticSeasons(t *testing.T) {
	seasons := normalizeSeasons(nil, []episodeRecord{{
		ID:           1,
		Number:       1,
		SeasonNumber: 2,
	}})
	if len(seasons) != 1 {
		t.Fatalf("len(seasons) = %d, want 1", len(seasons))
	}
	if seasons[0].ProviderRef != "" {
		t.Fatalf("ProviderRef = %q, want empty synthetic ref", seasons[0].ProviderRef)
	}
}
