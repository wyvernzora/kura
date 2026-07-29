package main

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/wyvernzora/kura/cli/internal/cli/client"
)

func TestTapeErrorPreservesRefusalCodeExactly(t *testing.T) {
	err := tapeError(&client.ErrorEnvelope{
		Status:   409,
		Kind:     "divergence_deferred",
		Category: "invalid_params",
		Message:  "divergence handling is deferred",
	})
	const want = "divergence_deferred: divergence handling is deferred"
	if err == nil || err.Error() != want {
		t.Fatalf("tapeError() = %v, want %q", err, want)
	}
}

func TestTapeErrorLeavesTransportErrorsUnchanged(t *testing.T) {
	transportErr := errors.New("transport failed")
	if got := tapeError(transportErr); !errors.Is(got, transportErr) {
		t.Fatalf("tapeError() = %v, want original transport error", got)
	}
}

func TestTapeErrorPreservesUnauthorizedBearerTokenHint(t *testing.T) {
	envelope := &client.ErrorEnvelope{
		Status:   http.StatusUnauthorized,
		Kind:     "unauthorized",
		Category: "invalid_params",
		Message:  "missing or invalid bearer token",
	}
	const hint = "kura server requires a bearer token; set KURA_TOKEN"
	withHint := fmt.Errorf("%w\n  hint: %s", envelope, hint)

	got := tapeError(withHint)
	const want = "missing or invalid bearer token\n  hint: " + hint
	if got == nil || got.Error() != want {
		t.Fatalf("tapeError() = %v, want %q", got, want)
	}
}
