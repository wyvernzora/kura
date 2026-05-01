package ui

import (
	"testing"

	"github.com/wyvernzora/kura/internal/progress"
)

func TestProgressMessagePrefixesCounter(t *testing.T) {
	event := progress.Event{
		Message: "Inspecting Bookworm - S01E01 (WebRip 1080p).mkv",
		Current: 1,
		Total:   2,
	}
	if got := progressMessage(event); got != "[1/2] Inspecting Bookworm - S01E01 (WebRip 1080p).mkv" {
		t.Fatalf("progressMessage = %q", got)
	}
}

func TestProgressMessageSkipsEmptyCounter(t *testing.T) {
	event := progress.Event{Message: "Scanning Bookworm"}
	if got := progressMessage(event); got != "Scanning Bookworm" {
		t.Fatalf("progressMessage = %q", got)
	}
}
