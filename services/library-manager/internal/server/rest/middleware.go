package rest

import (
	"bytes"
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

// maxLoggedErrorBody caps how much of an error response body is
// captured for the request log. Error envelopes already truncate their
// message to 512 bytes; this bound keeps log lines from ballooning if
// a handler ever streams a large non-2xx body.
const maxLoggedErrorBody = 1024

// loggingMiddleware emits one structured log line per request. Level
// tracks the outcome — Error for 5xx, Warn for 4xx, Debug for probe
// noise on /healthz, Info otherwise — and non-2xx responses carry the
// error envelope the client saw, so a 500/503 in the log names its
// cause without correlating across systems. No-op when logger is nil.
func loggingMiddleware(logger *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		if logger == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rec := &recordingWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			level := slog.LevelInfo
			switch {
			case rec.status >= 500:
				level = slog.LevelError
			case rec.status >= 400:
				level = slog.LevelWarn
			case r.URL.Path == "/healthz":
				level = slog.LevelDebug
			}
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(started).Milliseconds(),
				"bytes", rec.bytes,
			}
			if q := r.URL.RawQuery; q != "" {
				attrs = append(attrs, "query", q)
			}
			if rec.status >= 400 && rec.errBody.Len() > 0 {
				attrs = append(attrs, "error_body", rec.errBody.String())
			}
			logger.Log(r.Context(), level, "rest request", attrs...)
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

// recordingWriter captures the status code, response size, and — for
// non-2xx responses — a bounded copy of the body so loggingMiddleware
// can log the error envelope after the handler returns.
type recordingWriter struct {
	http.ResponseWriter
	status  int
	bytes   int
	errBody bytes.Buffer
}

func (rw *recordingWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recordingWriter) Write(p []byte) (int, error) {
	if rw.status >= 400 && rw.errBody.Len() < maxLoggedErrorBody {
		rw.errBody.Write(p[:min(len(p), maxLoggedErrorBody-rw.errBody.Len())])
	}
	n, err := rw.ResponseWriter.Write(p)
	rw.bytes += n
	return n, err
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
