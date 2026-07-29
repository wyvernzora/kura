package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyvernzora/kura/services/tape-backup/internal/executor"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(
		context.Background(),
		[]string{"-version"},
		os.Getenv,
		&stdout,
		&stderr,
	); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got, want := stdout.String(), version+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunServeLoadsConfigAndStopsAtHardwareBoundary(t *testing.T) {
	configPath := writeConfig(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"serve", "-config", configPath},
		func(name string) string {
			if name == "KURA_TOKEN" {
				return "test-token"
			}
			return ""
		},
		&stdout,
		&stderr,
	)
	if !errors.Is(err, executor.ErrHardwareDriveNotImplemented) {
		t.Fatalf("run() error = %v, want ErrHardwareDriveNotImplemented", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `"source":"environment"`) {
		t.Fatalf("auth log = %q, want environment source", stderr.String())
	}
}

func TestRunAcceptsFlagsBeforeSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(
		context.Background(),
		[]string{"-config", writeConfig(t), "serve"},
		func(string) string { return "test-token" },
		&stdout,
		&stderr,
	)
	if !errors.Is(err, executor.ErrHardwareDriveNotImplemented) {
		t.Fatalf("run() error = %v, want ErrHardwareDriveNotImplemented", err)
	}
}

func TestRunRejectsMissingSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), nil, os.Getenv, &stdout, &stderr)
	if err == nil || err.Error() != "expected subcommand serve" {
		t.Fatalf("run() error = %v, want %q", err, "expected subcommand serve")
	}
}

func TestRunRejectsDeletedRunEntrypoint(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), []string{"run"}, os.Getenv, &stdout, &stderr)
	if err == nil || err.Error() != "expected subcommand serve" {
		t.Fatalf("run() error = %v, want %q", err, "expected subcommand serve")
	}
}

func writeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tape-backup.toml")
	body := `
[library]
root = "/library"
url = "http://library"

[state]
root = "/state"

[auth]
token_path = "` + filepath.Join(t.TempDir(), "token") + `"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
