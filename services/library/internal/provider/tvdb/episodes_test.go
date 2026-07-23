package tvdb

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/provider"
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

	_, err = p.client.seriesEpisodes(context.Background(), "1", "")
	if !errors.Is(err, provider.ErrUnavailable) {
		t.Fatalf("seriesEpisodes error = %v, want provider.ErrUnavailable", err)
	}
}

func TestSeriesEpisodesUsesOrderingPathSegment(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ordering   string
		wantSuffix string
	}{
		{"empty defaults to default", "", "/series/1/episodes/default"},
		{"explicit default", "default", "/series/1/episodes/default"},
		{"dvd", "dvd", "/series/1/episodes/dvd"},
		{"absolute", "absolute", "/series/1/episodes/absolute"},
		{"alternate", "alternate", "/series/1/episodes/alternate"},
		{"official", "official", "/series/1/episodes/official"},
		{"regional", "regional", "/series/1/episodes/regional"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			mux := http.NewServeMux()
			mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, map[string]any{
					"status": "success",
					"data":   map[string]any{"token": "token"},
				})
			})
			mux.HandleFunc(tc.wantSuffix, func(w http.ResponseWriter, r *http.Request) {
				requireAuth(t, r)
				got = r.URL.Path
				writeJSON(t, w, map[string]any{
					"status": "success",
					"data":   map[string]any{"episodes": []map[string]any{}},
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
			if _, err := p.client.seriesEpisodes(context.Background(), "1", tc.ordering); err != nil {
				t.Fatalf("seriesEpisodes: %v", err)
			}
			if got != tc.wantSuffix {
				t.Fatalf("path = %q, want %q", got, tc.wantSuffix)
			}
		})
	}
}

func TestNormalizeEpisodeRecordNormalizesAiredDate(t *testing.T) {
	got := normalizeEpisodeRecord(episodeRecord{
		ID:           1,
		Aired:        "not-a-date",
		FirstAired:   "2020-01-02",
		Number:       3,
		SeasonNumber: 2,
	}, 2, nil)

	if got.Aired != "2020-01-02" {
		t.Fatalf("Aired = %q, want fallback firstAired date", got.Aired)
	}
}

func TestNormalizeSeasonsUsesEmptyMetadataRefForSyntheticSeasons(t *testing.T) {
	seasons := normalizeSeasons(nil, []episodeRecord{{
		ID:           1,
		Number:       1,
		SeasonNumber: 2,
	}}, nil)
	if len(seasons) != 1 {
		t.Fatalf("len(seasons) = %d, want 1", len(seasons))
	}
	if seasons[0].MetadataRef != "" {
		t.Fatalf("MetadataRef = %q, want empty synthetic ref", seasons[0].MetadataRef)
	}
}
