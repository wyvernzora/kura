package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	infohash = "0123456789abcdef0123456789abcdef01234567"
	magnet   = "magnet:?xt=urn:btih:" + infohash + "&tr=udp://tracker.product/announce"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	mux.HandleFunc("POST /login", login)
	mux.HandleFunc("GET /series/12345/extended", series)
	mux.HandleFunc("GET /series/12345/episodes/default", episodes)
	mux.HandleFunc("GET /topics/list/sort_id/2/page/{page}", dmhy)

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"apikey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "failure"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   map[string]any{"token": "product-e2e-token"},
	})
}

func series(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "failure"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data": map[string]any{
			"id":                12345,
			"name":              "Product Series",
			"slug":              "product-series",
			"firstAired":        "2026-01-01",
			"lastAired":         "2026-01-08",
			"originalCountry":   "usa",
			"originalLanguage":  "eng",
			"defaultSeasonType": 1,
			"status":            map[string]any{"name": "Continuing"},
			"genres":            []map[string]any{{"name": "Anime"}},
			"translations": map[string]any{
				"nameTranslations": []map[string]any{
					{"language": "eng", "name": "Product Series", "isPrimary": true},
				},
			},
			"aliases": []map[string]any{
				{"language": "eng", "name": "Kura Product Fixture"},
			},
			"seasons": []map[string]any{
				{"id": 501, "number": 1, "name": "Season 1"},
			},
		},
	})
}

func episodes(w http.ResponseWriter, r *http.Request) {
	if !authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "failure"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data": map[string]any{
			"episodes": []map[string]any{
				{
					"id":             7001,
					"name":           "The Product Surface",
					"aired":          "2026-01-01",
					"number":         1,
					"seasonNumber":   1,
					"absoluteNumber": 1,
				},
			},
		},
		"links": map[string]any{
			"prev": nil,
			"next": nil,
			"page": 0,
		},
	})
}

func dmhy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.PathValue("page") != "1" {
		_, _ = w.Write([]byte(archivePage()))
		return
	}
	_, _ = w.Write([]byte(archivePage(fmt.Sprintf(`<tr class="">
<td class="title"><a href="/topics/view/9001_product.html">Product Series - 01 [1080p]</a></td>
<td><a class="arrow-magnet" href="%s">download</a><a data-magnet="magnet:?xt=urn:btih:%s"></a></td>
<td align="center">8MB</td>
<td><span style="display: none;">2026/01/01 12:00</span></td>
</tr>`, magnet, infohash))))
}

func archivePage(rows ...string) string {
	return `<!doctype html><html><body><table id="topic_list"><tbody>` +
		strings.Join(rows, "\n") +
		`</tbody></table></body></html>`
}

func authorized(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer product-e2e-token"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
