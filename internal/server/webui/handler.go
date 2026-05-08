package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// Handler returns an HTTP handler that serves the embedded SPA bundle.
//
// Routing:
//
//   - Paths under /api/ return 404 defensively. The REST mux's API
//     patterns are more specific and should already match first; this
//     guards against accidental fall-through if a future API verb is
//     forgotten in the mux.
//   - Existing files in dist/ are served via http.FileServer.
//   - Anything else falls back to index.html so the SPA router can
//     resolve client-side routes on a hard refresh.
//
// Cache headers:
//
//   - Vite emits content-hashed filenames under assets/, so anything
//     in that subtree is safely immutable for one year.
//   - index.html and any non-hashed file ship with no-cache so a new
//     deploy propagates immediately.
//
// HEAD is treated like GET for the SPA fallback so curl -I works.
// Other methods get 405.
func Handler() http.Handler {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		// distFS is built at compile time from a directory we control,
		// so a missing subtree means a programming error, not a runtime
		// condition we can recover from.
		panic("webui: dist subtree missing: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(dist))
	indexHTML, _ := fs.ReadFile(dist, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		clean := path.Clean(r.URL.Path)
		p := strings.TrimPrefix(clean, "/")

		if p != "" {
			if _, err := fs.Stat(dist, p); err != nil {
				serveIndex(w, r, indexHTML)
				return
			}
		}

		if isHashedAsset(p) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if len(body) == 0 {
		http.Error(w, "web UI not built", http.StatusServiceUnavailable)
		return
	}
	_, _ = w.Write(body)
}

// isHashedAsset reports whether the path points at content-hashed,
// long-cacheable bundler output. Vite emits these under assets/ by
// default.
func isHashedAsset(p string) bool {
	return strings.HasPrefix(p, "assets/")
}
