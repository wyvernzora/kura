package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerMiddlewareRejectsMissingTokenExactly(t *testing.T) {
	handler := BearerMiddleware("secret")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("authenticated handler was called")
	}))
	request := httptest.NewRequest(http.MethodGet, "/api/tape/status", http.NoBody)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	const want = "{\"kind\":\"unauthorized\",\"category\":\"invalid_params\",\"message\":\"missing or invalid bearer token\"}\n"
	if response.Body.String() != want {
		t.Fatalf("body = %q, want %q", response.Body.String(), want)
	}
	if got := response.Header().Get("WWW-Authenticate"); got != `Bearer realm="kura"` {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, `Bearer realm="kura"`)
	}
}

func TestBearerMiddlewareAllowsTokenAndUnauthenticatedHealth(t *testing.T) {
	handler := BearerMiddleware("secret")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, test := range []struct {
		name   string
		path   string
		token  string
		status int
	}{
		{name: "token", path: "/api/tape/status", token: "Bearer secret", status: http.StatusNoContent},
		{name: "health", path: HealthPath, status: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, http.NoBody)
			request.Header.Set("Authorization", test.token)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}
