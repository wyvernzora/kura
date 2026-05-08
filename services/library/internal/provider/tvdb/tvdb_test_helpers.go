package tvdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const testTVDBBaseURL = "http://tvdb.test"

type testHTTPServer struct {
	URL    string
	client *http.Client
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newTestServer(t *testing.T, override func(http.ResponseWriter, *http.Request) bool) *testHTTPServer {
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
						{"language": "jpn", "name": "本好きの下剋上", "aliases": []string{"非公式別名"}, "isPrimary": true},
						{"language": "eng", "name": "Official English Title", "isPrimary": true},
						{"language": "eng", "name": "English Alias", "isAlias": true},
						{"language": "jpn", "name": "非公式別名", "isAlias": true},
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

	return newTestHTTPServer(t, mux)
}

func newLocalHTTPServer(t *testing.T, handler http.Handler) *testHTTPServer {
	return newTestHTTPServer(t, handler)
}

func newTestHTTPServer(t *testing.T, handler http.Handler) *testHTTPServer {
	t.Helper()

	parsedBaseURL, err := url.Parse(testTVDBBaseURL)
	if err != nil {
		t.Fatalf("parse test base URL: %v", err)
	}

	server := &testHTTPServer{
		URL: parsedBaseURL.String(),
		client: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				recorder := httptest.NewRecorder()
				req.URL.Host = parsedBaseURL.Host
				req.Host = parsedBaseURL.Host
				handler.ServeHTTP(recorder, req)
				return recorder.Result(), nil
			}),
		},
	}
	return server
}

func (s *testHTTPServer) Client() *http.Client {
	return s.client
}

func (s *testHTTPServer) Close() {}

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
