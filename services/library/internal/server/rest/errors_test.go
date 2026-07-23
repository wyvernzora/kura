package rest

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wyvernzora/kura/services/library/internal/errkind"
)

// fakeCoded implements errkind.Coded with chosen kind/category for
// status-mapping tests.
type fakeCoded struct {
	msg      string
	kind     string
	category string
	data     map[string]any
}

func (e *fakeCoded) Error() string        { return e.msg }
func (e *fakeCoded) Kind() string         { return e.kind }
func (e *fakeCoded) Category() string     { return e.category }
func (e *fakeCoded) Data() map[string]any { return e.data }

func TestEncodeError_KindToStatus(t *testing.T) {
	cases := []struct {
		name string
		kind string
		want int
	}{
		{"not_found", errkind.KindNotFound, http.StatusNotFound},
		{"conflict", errkind.KindConflict, http.StatusConflict},
		{"busy", errkind.KindBusy, http.StatusConflict},
		{"plan_applied", errkind.KindPlanApplied, http.StatusConflict},
		{"stale_snapshot", errkind.KindStaleSnapshot, http.StatusConflict},
		{"invalid_episode", errkind.KindInvalidEpisode, http.StatusUnprocessableEntity},
		{"no_staged", errkind.KindNoStaged, http.StatusUnprocessableEntity},
		{"unsupported_provider", errkind.KindUnsupportedProvider, http.StatusUnprocessableEntity},
		{"invalid_cursor", errkind.KindInvalidCursor, http.StatusUnprocessableEntity},
		{"server_not_ready", errkind.KindServerNotReady, http.StatusServiceUnavailable},
		{"claim_stolen", errkind.KindClaimStolen, http.StatusInternalServerError},
		{"invalid_ref", errkind.KindInvalidRef, http.StatusBadRequest},
		{"provider_unavailable", errkind.KindProviderUnavailable, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &fakeCoded{msg: "x", kind: tc.kind, category: errkind.CategoryInvalidParams}
			status, env := encodeError(err)
			if status != tc.want {
				t.Errorf("kind=%q status: got %d want %d", tc.kind, status, tc.want)
			}
			if env.Kind != tc.kind {
				t.Errorf("envelope kind: got %q want %q", env.Kind, tc.kind)
			}
		})
	}
}

func TestEncodeError_ValidationError(t *testing.T) {
	status, env := encodeError(&validationError{msg: "bad input"})
	if status != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", status)
	}
	if env.Kind != errkind.KindInvalidRef {
		t.Errorf("kind: got %q want %q", env.Kind, errkind.KindInvalidRef)
	}
	if env.Message != "bad input" {
		t.Errorf("message: got %q", env.Message)
	}
}

func TestEncodeError_ForbiddenError(t *testing.T) {
	status, env := encodeError(&forbiddenError{msg: "no operator header"})
	if status != http.StatusForbidden {
		t.Errorf("status: got %d want 403", status)
	}
	if env.Kind != "forbidden" {
		t.Errorf("kind: got %q want forbidden", env.Kind)
	}
}

func TestEncodeError_FallbackInternal(t *testing.T) {
	status, env := encodeError(errors.New("untyped"))
	if status != http.StatusInternalServerError {
		t.Errorf("status: got %d want 500", status)
	}
	if env.Kind != errkind.KindInternal {
		t.Errorf("kind: got %q want %q", env.Kind, errkind.KindInternal)
	}
}

func TestEncodeError_TruncatesLongMessage(t *testing.T) {
	long := make([]byte, maxInternalMessage*2)
	for i := range long {
		long[i] = 'a'
	}
	_, env := encodeError(errors.New(string(long)))
	if len(env.Message) != maxInternalMessage {
		t.Errorf("message length: got %d want %d", len(env.Message), maxInternalMessage)
	}
}

func TestWriteError_RendersJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, &validationError{msg: "x"})
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content-type: got %q", got)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d want 400", rec.Code)
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Kind != errkind.KindInvalidRef {
		t.Errorf("kind: got %q", env.Kind)
	}
}
