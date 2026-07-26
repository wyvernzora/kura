package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if got, want := stdout.String(), version+"\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunEntrypointsLoadConfigAndStopAtScaffold(t *testing.T) {
	configPath := writeConfig(t)
	for _, command := range []string{"serve", "run"} {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := run([]string{command, "-config", configPath}, &stdout, &stderr)
			if err == nil || err.Error() != command+" is not implemented" {
				t.Fatalf("run() error = %v, want not implemented", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			logLine := stderr.String()
			if !strings.Contains(logLine, `"service":"kura-tape-backup"`) ||
				!strings.Contains(logLine, `"mode":"`+command+`"`) {
				t.Fatalf("startup log = %q", logLine)
			}
		})
	}
}

func TestRunAcceptsFlagsBeforeSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"-config", writeConfig(t), "serve"}, &stdout, &stderr)
	if err == nil || err.Error() != "serve is not implemented" {
		t.Fatalf("run() error = %v, want not implemented", err)
	}
}

func TestRunRejectsMissingSubcommand(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(nil, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "expected subcommand") {
		t.Fatalf("run() error = %v, want subcommand error", err)
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
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
