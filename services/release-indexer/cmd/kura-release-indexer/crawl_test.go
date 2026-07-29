package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// The crawl subcommand round-trip: one fixture page in, ingest-ready JSONL
// out, cursor on stderr, empty page signalling the end of the listing.
func TestRunCrawlCommand(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, filepath.Join("..", "..", "sources", "nyaa", "testdata", "live-listing-p2.html"), filepath.Join(dir, "page-1.html"))
	copyFixture(t, filepath.Join("..", "..", "sources", "nyaa", "testdata", "live-no-results.html"), filepath.Join(dir, "page-2.html"))

	// The source is deliberately disabled: crawl is an operator command and
	// must work without the scheduled loop's required keys.
	configPath := filepath.Join(dir, "release-indexer.toml")
	config := "[sources.nyaa]\nenabled = false\nurl = \"file://" + dir + "\"\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runCrawlCommand([]string{"-config", configPath, "-source", "nyaa", "-page", "1"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runCrawlCommand() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("crawl emitted no posts for the listing fixture")
	}
	for _, line := range lines {
		var post api.RawPost
		if err := json.Unmarshal([]byte(line), &post); err != nil {
			t.Fatalf("stdout line is not a RawPost: %v\n%s", err, line)
		}
		if post.Source != api.SourceNyaa || post.SourceID == "" || post.Magnet == "" || post.PublishedAt.IsZero() {
			t.Fatalf("post missing ingest-required fields: %+v", post)
		}
	}
	if !strings.Contains(stderr.String(), "next_page=2") {
		t.Fatalf("stderr = %q, want next_page=2 cursor", stderr.String())
	}

	// Past the end: empty stdout, a note on stderr, exit success.
	stdout.Reset()
	stderr.Reset()
	err = runCrawlCommand([]string{"-config", configPath, "-source", "nyaa", "-page", "2"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runCrawlCommand(page 2) error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty for a no-results page", stdout.String())
	}
	if !strings.Contains(stderr.String(), "empty page") {
		t.Fatalf("stderr = %q, want empty-page note", stderr.String())
	}
}

func TestRunCrawlCommandRejectsBadArguments(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "release-indexer.toml")
	if err := os.WriteFile(configPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := runCrawlCommand([]string{"-config", configPath, "-source", "vhs"}, &stdout, &stderr); err == nil ||
		!strings.Contains(err.Error(), "-source must be") {
		t.Fatalf("unknown source error = %v", err)
	}
	if err := runCrawlCommand([]string{"-config", configPath, "-source", "nyaa", "-page", "0"}, &stdout, &stderr); err == nil ||
		!strings.Contains(err.Error(), "-page must be >= 1") {
		t.Fatalf("page 0 error = %v", err)
	}
}

func TestRunCrawlCommandRejectsInvalidDisabledSource(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "release-indexer.toml")
	config := "[sources.dmhy]\nenabled = false\ncategory = \"-1\"\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	err := runCrawlCommand([]string{"-config", configPath, "-source", "dmhy"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "category must be a non-negative integer string") {
		t.Fatalf("runCrawlCommand() error = %v, want invalid category error", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no posts for invalid source config", stdout.String())
	}
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", dst, err)
	}
}
