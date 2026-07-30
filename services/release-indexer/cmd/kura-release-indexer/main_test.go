package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestWriteCommandErrorUsesReleaseIndexerName(t *testing.T) {
	var stderr bytes.Buffer
	writeCommandError(&stderr, errors.New("boom"))
	if got, want := stderr.String(), "kura-release-indexer: boom\n"; got != want {
		t.Fatalf("writeCommandError() = %q, want %q", got, want)
	}
}
