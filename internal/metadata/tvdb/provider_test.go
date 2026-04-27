package tvdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/metadata"
)

func TestSearchNormalizesSeriesResults(t *testing.T) {
	server := newTestServer(t, nil)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:            server.URL,
		HTTPClient:         server.Client(),
		PreferredLanguages: []string{"ja", "en"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := p.Search(context.Background(), "honzuki", metadata.SearchOptions{
		Limit: 5,
		Year:  2019,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	got := results[0]
	if got.ProviderRef != "tvdb:370070" {
		t.Fatalf("ProviderRef = %q, want tvdb:370070", got.ProviderRef)
	}
	if !slices.Equal(got.ProviderRefs, []string{"tvdb:370070", "imdb:tt10885406", "tmdb:12345"}) {
		t.Fatalf("ProviderRefs = %#v, want tvdb/imdb/tmdb refs", got.ProviderRefs)
	}
	if got.CanonicalTitle != "Ascendance of a Bookworm" {
		t.Fatalf("CanonicalTitle = %q", got.CanonicalTitle)
	}
	if got.PreferredTitle != "本好きの下剋上" {
		t.Fatalf("PreferredTitle = %q, want 本好きの下剋上", got.PreferredTitle)
	}
	if got.Year != 2019 {
		t.Fatalf("Year = %d, want 2019", got.Year)
	}
	if got.Status != metadata.SeriesStatusEnded {
		t.Fatalf("Status = %q, want ended", got.Status)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "Fantasy" || got.Genres[1] != "Anime" {
		t.Fatalf("Genres = %#v, want Fantasy, Anime", got.Genres)
	}
	if got.OriginalLanguage != "ja" {
		t.Fatalf("OriginalLanguage = %q, want ja", got.OriginalLanguage)
	}
	if got.OriginalCountry != "JP" {
		t.Fatalf("OriginalCountry = %q, want JP", got.OriginalCountry)
	}
	if got.FirstAired != "2019-10-03" {
		t.Fatalf("FirstAired = %#v, want 2019-10-03", got.FirstAired)
	}
}

func TestSearchUsesCanonicalTitleWhenNoPreferredLanguages(t *testing.T) {
	server := newTestServer(t, nil)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := p.Search(context.Background(), "honzuki", metadata.SearchOptions{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].PreferredTitle != "Ascendance of a Bookworm" {
		t.Fatalf("PreferredTitle = %q, want canonical title", results[0].PreferredTitle)
	}
}

func TestGetSeriesAggregatesExtendedAndEpisodes(t *testing.T) {
	server := newTestServer(t, nil)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:            server.URL,
		HTTPClient:         server.Client(),
		PreferredLanguages: []string{"ja", "en"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	series, err := p.GetSeries(context.Background(), "370070")
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	if series.ProviderRef != "tvdb:370070" {
		t.Fatalf("ProviderRef = %q, want tvdb:370070", series.ProviderRef)
	}
	if !slices.Equal(series.ProviderRefs, []string{"tvdb:370070", "imdb:tt10885406", "tmdb:12345"}) {
		t.Fatalf("ProviderRefs = %#v, want tvdb/imdb/tmdb refs", series.ProviderRefs)
	}
	if series.CanonicalTitle != "Ascendance of a Bookworm" {
		t.Fatalf("CanonicalTitle = %q", series.CanonicalTitle)
	}
	if series.PreferredTitle != "本好きの下剋上" {
		t.Fatalf("PreferredTitle = %q, want ja title", series.PreferredTitle)
	}
	if series.OriginalLanguage != "ja" {
		t.Fatalf("OriginalLanguage = %q, want ja", series.OriginalLanguage)
	}
	if series.OriginalCountry != "JP" {
		t.Fatalf("OriginalCountry = %q, want JP", series.OriginalCountry)
	}
	if series.FirstAired != "2019-10-03" || series.LastAired != "2022-06-14" {
		t.Fatalf("FirstAired/LastAired = %q/%q, want 2019-10-03/2022-06-14", series.FirstAired, series.LastAired)
	}
	if len(series.Seasons) != 1 {
		t.Fatalf("len(Seasons) = %d, want 1", len(series.Seasons))
	}
	if series.Seasons[0].Number != 1 {
		t.Fatalf("season number = %d, want 1", series.Seasons[0].Number)
	}
	if got := series.Seasons[0].Episodes[0]; got.ProviderRef != "tvdb:1001" || got.EpisodeNumber != 1 {
		t.Fatalf("first season 1 episode = %#v", got)
	}
	if series.Seasons[0].Episodes[0].AbsoluteNumber == nil || *series.Seasons[0].Episodes[0].AbsoluteNumber != 1 {
		t.Fatalf("AbsoluteNumber = %#v, want 1", series.Seasons[0].Episodes[0].AbsoluteNumber)
	}
	if got := series.Seasons[0].Episodes[0].Aired; got != "2019-10-03" {
		t.Fatalf("Aired = %q, want 2019-10-03", got)
	}
	if len(series.Specials) != 1 {
		t.Fatalf("len(Specials) = %d, want 1", len(series.Specials))
	}
	if got := series.Specials[0]; got.ProviderRef != "tvdb:9001" || got.SeasonNumber != 0 || got.EpisodeNumber != 1 {
		t.Fatalf("first special = %#v", got)
	}
}

func TestSelectTitleUsesCanonicalAsOriginalLanguageFallback(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja", "en"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	title := p.selectTitle("日本語タイトル", "jpn", []titleCandidate{
		{Language: "eng", Value: "English Title"},
	})

	if title != "日本語タイトル" {
		t.Fatalf("title = %q, want canonical ja fallback", title)
	}
}

func TestSelectTitlePrefersExplicitOriginalLanguageTranslation(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja", "en"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	title := p.selectTitle("Provider Canonical", "jpn", []titleCandidate{
		{Language: "jpn", Value: "日本語タイトル"},
		{Language: "eng", Value: "English Title"},
	})

	if title != "日本語タイトル" {
		t.Fatalf("title = %q, want explicit ja translation", title)
	}
}

func TestSelectTitleFallsBackToNextPreferredLanguage(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja", "en"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	title := p.selectTitle("Provider Canonical", "eng", []titleCandidate{
		{Language: "eng", Value: "English Title"},
	})

	if title != "English Title" {
		t.Fatalf("title = %q, want en translation", title)
	}
}

func TestNormalizeSeriesSummaryNormalizesProviderTitlesToNFC(t *testing.T) {
	p, err := New("test-key", Options{
		PreferredLanguages: []string{"ja"},
		HTTPClient:         http.DefaultClient,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	summary := p.normalizeSeriesSummary(
		"tvdb:1",
		"本好きの下剋上 司書になるためには手段を選んでいられません",
		"jpn",
		"JP",
		"2019-10-03",
		metadata.SeriesStatusContinuing,
		2019,
		nil,
		nil,
		[]titleCandidate{
			{Language: "jpn", Value: "本好きの下剋上 司書になるためには手段を選んでいられません"},
		},
	)

	want := "本好きの下剋上 司書になるためには手段を選んでいられません"
	if summary.PreferredTitle != want {
		t.Fatalf("PreferredTitle = %q, want %q", summary.PreferredTitle, want)
	}
	if summary.CanonicalTitle != want {
		t.Fatalf("CanonicalTitle = %q, want %q", summary.CanonicalTitle, want)
	}
}

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

	if _, err := p.Search(context.Background(), "honzuki", metadata.SearchOptions{}); err != nil {
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
	server := httptest.NewServer(mux)
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
			_, err := p.Search(context.Background(), "honzuki", metadata.SearchOptions{})
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
	server := httptest.NewServer(mux)
	defer server.Close()

	p, err := New("test-key", Options{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = p.client.seriesEpisodes(context.Background(), "1")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("seriesEpisodes error = %v, want ErrUnavailable", err)
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
	seasons, _ := normalizeSeasons(nil, []episodeRecord{{
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

func newTestServer(t *testing.T, override func(http.ResponseWriter, *http.Request) bool) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("login method = %s, want POST", r.Method)
		}
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"token": "token",
			},
		})
	})
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		if override != nil && override(w, r) {
			return
		}
		requireAuth(t, r)
		if got := r.URL.Query().Get("query"); got != "honzuki" {
			t.Fatalf("query = %q, want honzuki", got)
		}
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": []map[string]any{
				{
					"objectID":         "series-370070",
					"id":               "series-370070",
					"tvdb_id":          "370070",
					"name":             "Ascendance of a Bookworm",
					"name_translated":  []string{"本好きの下剋上"},
					"aliases":          []string{"Honzuki no Gekokujou"},
					"type":             "series",
					"status":           "Ended",
					"year":             2019,
					"first_air_time":   "2019-10-03",
					"primary_language": "jpn",
					"country":          "jpn",
					"remote_ids": []map[string]any{
						{"id": "tt10885406", "sourceName": "IMDB"},
						{"id": "12345", "sourceName": "TheMovieDB.com"},
					},
					"translations": map[string]any{
						"jpn": "本好きの下剋上",
					},
					"genres": []string{"Fantasy", "Anime"},
					"score":  42.5,
				},
				{
					"id":   "movie-1",
					"name": "Not a Series",
					"type": "movie",
				},
			},
			"links": map[string]any{
				"prev": nil,
				"self": "https://api4.thetvdb.com/v4/search?query=honzuki&limit=1&page=0",
				"next": nil,
			},
		})
	})
	mux.HandleFunc("/series/370070/extended", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"id":               370070,
				"name":             "Ascendance of a Bookworm",
				"firstAired":       "2019-10-03",
				"lastAired":        "2022-06-14",
				"originalCountry":  "jpn",
				"originalLanguage": "jpn",
				"averageRuntime":   24,
				"status":           map[string]any{"name": "Ended"},
				"genres": []map[string]any{
					{"name": "Fantasy"},
				},
				"remoteIds": []map[string]any{
					{"id": "tt10885406", "sourceName": "IMDB"},
					{"id": "12345", "sourceName": "TheMovieDB.com"},
				},
				"translations": map[string]any{
					"nameTranslations": []map[string]any{
						{"language": "jpn", "name": "本好きの下剋上", "aliases": []string{"非公式別名"}},
						{"language": "eng", "name": "Official English Title"},
						{"language": "eng", "name": "English Alias"},
						{"language": "jpn", "name": "非公式別名"},
						{"language": "", "name": "jpn"},
					},
				},
				"nameTranslations": []string{"jpn", "eng"},
				"aliases": []map[string]any{
					{"language": "eng", "name": "Honzuki"},
				},
				"seasons": []map[string]any{
					{"id": 10, "number": 1, "name": "Season 1"},
					{"id": 11, "number": 1, "name": "Season 1 Duplicate"},
					{"id": 9, "number": 0, "name": "Specials"},
				},
			},
		})
	})
	mux.HandleFunc("/series/370070/episodes/default", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		writeJSON(t, w, map[string]any{
			"status": "success",
			"data": map[string]any{
				"episodes": []map[string]any{
					{
						"id":             1002,
						"name":           "Apprentice Priestess",
						"aired":          "2019-10-10",
						"number":         2,
						"seasonNumber":   1,
						"absoluteNumber": 2,
						"runtime":        24,
					},
					{
						"id":             9001,
						"name":           "OVA",
						"aired":          "2020-03-10",
						"number":         1,
						"seasonNumber":   0,
						"absoluteNumber": 0,
						"runtime":        24,
					},
					{
						"id":             1001,
						"name":           "A World Without Books",
						"aired":          "2019-10-03",
						"number":         1,
						"seasonNumber":   1,
						"absoluteNumber": 1,
						"runtime":        24,
					},
				},
			},
			"links": map[string]any{},
		})
	})

	return httptest.NewServer(mux)
}

func requireAuth(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != "Bearer token" {
		t.Fatalf("Authorization = %q, want Bearer token", got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}
