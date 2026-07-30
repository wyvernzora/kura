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
	"github.com/wyvernzora/kura/services/tape-backup/internal/server/auth"
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

func TestRunServeValidatesKeyAtStartupWithoutLoggingContents(t *testing.T) {
	for _, test := range []struct {
		name       string
		contents   string
		wantLogged string
	}{
		{
			name:       "missing",
			wantLogged: "encryption key: configured key file does not exist",
		},
		{
			name:       "malformed",
			contents:   "THIS-MUST-NEVER-APPEAR-IN-THE-LOG",
			wantLogged: "encryption key: configured key file must contain exactly 64 lowercase hexadecimal characters with one optional trailing newline",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			keyPath := filepath.Join(t.TempDir(), "tape.key")
			if test.contents != "" {
				if err := os.WriteFile(keyPath, []byte(test.contents), 0o600); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			}
			configPath := writeConfig(t)
			file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatalf("OpenFile() error = %v", err)
			}
			if _, err := file.WriteString(
				"\n[encryption]\nkey_file = \"" + keyPath + "\"\n",
			); err != nil {
				_ = file.Close()
				t.Fatalf("WriteString() error = %v", err)
			}
			if err := file.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err = run(
				context.Background(),
				[]string{"serve", "-config", configPath},
				func(string) string { return "test-token" },
				&stdout,
				&stderr,
			)
			if !errors.Is(err, executor.ErrHardwareDriveNotImplemented) {
				t.Fatalf(
					"run() error = %v, want ErrHardwareDriveNotImplemented",
					err,
				)
			}
			if !strings.Contains(stderr.String(), test.wantLogged) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), test.wantLogged)
			}
			if test.contents != "" && strings.Contains(stderr.String(), test.contents) {
				t.Fatalf("stderr exposed key file contents: %q", stderr.String())
			}
		})
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

func TestLogTokenStatusGeneratedDoesNotLogSecret(t *testing.T) {
	var output bytes.Buffer
	const (
		token     = "generated-secret-token"
		tokenPath = "/state/token"
	)
	logTokenStatus(
		newLogger(&output, "info"),
		auth.Result{Token: token, Generated: true},
		tokenPath,
	)

	logged := output.String()
	if strings.Contains(logged, token) {
		t.Fatalf("generated-token log contains bearer token: %q", logged)
	}
	if !strings.Contains(logged, `"path":"`+tokenPath+`"`) {
		t.Fatalf("generated-token log = %q, want token file path", logged)
	}
	const hint = `"hint":"read the token file and set KURA_TOKEN on clients"`
	if !strings.Contains(logged, hint) {
		t.Fatalf("generated-token log = %q, want %s", logged, hint)
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
