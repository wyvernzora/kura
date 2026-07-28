package rest

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"time"
)

const (
	headerVersion        = "X-Kura-Version"
	headerJobID          = "X-Kura-Job-Id"
	headerIfNoneMatch    = "If-None-Match"
	headerETag           = "ETag"
	headerCacheControl   = "Cache-Control"
	cacheControlReadable = "private, must-revalidate"
	cacheControlNoStore  = "no-store"
)

type middleware func(http.Handler) http.Handler

// loggingMiddleware emits one structured log line per request with
// method, path, status, duration. No-op when logger is nil.
func loggingMiddleware(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		if logger == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rec := &recordingWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("rest request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(started).Milliseconds(),
			)
		})
	}
}

// recoverMiddleware turns handler panics into a 500 internal-error
// envelope so a single bad handler doesn't drop the listener.
func recoverMiddleware(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if logger != nil {
						logger.Error("rest panic",
							"method", r.Method,
							"path", r.URL.Path,
							"panic", rec,
							"stack", string(debug.Stack()),
						)
					}
					writeError(w, &internalError{msg: "internal server error"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// corsMiddleware applies an allow-list-based CORS policy. Empty list =
// no CORS headers emitted (same-origin only). "*" allows any origin
// but is reflected back specifically (not "*") so cookie-bearing
// requests work if the deployer puts Kura behind a same-site proxy.
func corsMiddleware(allowedOrigins []string) middleware {
	return func(next http.Handler) http.Handler {
		if len(allowedOrigins) == 0 {
			// fast path: no CORS, but still answer OPTIONS preflight
			// with 204 so misconfigured browsers don't hang.
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (slices.Contains(allowedOrigins, "*") || slices.Contains(allowedOrigins, origin)) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+headerIfNoneMatch)
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// versionMiddleware stamps X-Kura-Version on every response. The
// version is captured at construction time so the chain doesn't dip
// into Server state per request.
func versionMiddleware(version string) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(headerVersion, version)
			next.ServeHTTP(w, r)
		})
	}
}

// recordingWriter captures the status code so loggingMiddleware can
// log it after the handler returns.
type recordingWriter struct {
	http.ResponseWriter
	status int
}

func (rw *recordingWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying ResponseWriter's Flusher so SSE
// streams flow through the logging middleware. Without this, the
// embedded ResponseWriter satisfies http.Flusher but recordingWriter
// itself does not, breaking the (Flusher) type assertion in the SSE
// handler.
func (rw *recordingWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
