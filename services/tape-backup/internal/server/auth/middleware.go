// Package auth provides the shared bearer-token gate for tape-backup HTTP
// endpoints.
package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
)

// HealthPath is the one path bearer auth never blocks.
const HealthPath = "/api/v1/health"

// BearerMiddleware enforces an exact Authorization bearer value. An empty
// token explicitly disables the gate.
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

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("WWW-Authenticate", `Bearer realm="kura"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(struct {
		Kind     string `json:"kind"`
		Category string `json:"category"`
		Message  string `json:"message"`
	}{
		Kind:     "unauthorized",
		Category: "invalid_params",
		Message:  "missing or invalid bearer token",
	})
}
