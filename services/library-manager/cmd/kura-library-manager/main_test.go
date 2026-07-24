package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func TestRunFlagErrors(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		err := run(context.Background(), []string{"--help"}, nil, io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("run(--help) = %v, want nil", err)
		}
	})

	t.Run("invalid flag", func(t *testing.T) {
		var stderr bytes.Buffer
		err := run(context.Background(), []string{"--bogus"}, nil, io.Discard, &stderr)
		if !errors.Is(err, errFlagAlreadyReported) {
			t.Fatalf("run(--bogus) = %v, want errFlagAlreadyReported", err)
		}
		if stderr.Len() == 0 {
			t.Fatal("run(--bogus) did not report the flag error")
		}
	})
}
