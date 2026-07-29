package main

import (
	"errors"
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
