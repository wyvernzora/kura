package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/wyvernzora/kura/internal/errkind"
)

// HealthPath is the one URL path bearer auth never blocks. K8s
// livenessProbe / readinessProbe hit it without auth headers; the
// REST server registers this exact path.
const HealthPath = "/api/v1/health"

// BearerMiddleware enforces "Authorization: Bearer <token>" on every
// request when token is non-empty. Empty token = auth disabled,
// passthrough. Used by both REST and MCP-HTTP transports so they
// share one consistent gate.
//
// The /api/v1/health path is exempt so liveness probes work without
// needing the secret. Other endpoints, including the MCP transport
// itself, remain gated.
//
// Constant-time compare avoids leaking the token's prefix via timing.
// 401 responses use the kura error envelope so kind=unauthorized is
// stable across transports.
func BearerMiddleware(token string) func(http.Handler) http.Handler {
	if token == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	want := []byte("Bearer " + token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == HealthPath {
				next.ServeHTTP(w, r)
				return
			}
			got := []byte(r.Header.Get("Authorization"))
			if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
				writeUnauthorized(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeUnauthorized emits the kura-shaped 401 envelope. Mirrors the
// REST error encoder so MCP-HTTP and REST 401s look identical to
// clients.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(struct {
		Kind     string `json:"kind"`
		Category string `json:"category"`
		Message  string `json:"message"`
	}{
		Kind:     "unauthorized",
		Category: errkind.CategoryInvalidParams,
		Message:  "missing or invalid bearer token",
	})
}
