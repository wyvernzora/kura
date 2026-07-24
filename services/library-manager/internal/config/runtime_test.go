package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "library-manager.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadRuntimeDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[library]
root = "/library"
inbox = "/inbox"
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := Defaults()
	want.Library.Root = "/library"
	want.Library.Inbox = "/inbox"
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Config =\n%+v\nwant\n%+v", cfg, want)
	}
}

func TestCommittedConfigsLoad(t *testing.T) {
	paths := []string{
		filepath.Join("..", "..", "config.example.toml"),
		filepath.Join("..", "..", "tools", "devserver", "library-manager.toml"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, err := Load(path); err != nil {
				t.Fatalf("Load(%q): %v", path, err)
			}
		})
	}
}

func TestLoadRuntimeAllFields(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[server]
rest = "127.0.0.1:9000"
mcp_http = ""
mcp_stdio = true
rest_cors_origins = ["https://kura.example"]
rest_port_file = "/tmp/kura-port"
log_level = "debug"
shutdown_timeout = "20s"
umask = "0007"

[library]
root = "/media/anime"
inbox = "/media/inbox"
airing_tail_days = 14

[metadata]
preferred_languages = ["en-US", "ja", "en-US"]
mediainfo_command = "/usr/local/bin/mediainfo"
tvdb_url = "http://tvdb.test"

[auth]
disabled = true
token_path = "/run/secrets/kura-token"

[jobs]
timeout = "1h"
retention = "45m"
reaper_interval = "3m"

[index]
probe_interval = "5s"
rebuild_interval = "2h"
library_root_debounce = "4s"

[sweep]
interval = "30m"
log_retention_days = 10

[coordination]
conflict_retries = 2
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Server.RESTAddr != "127.0.0.1:9000" ||
		cfg.Server.MCPHTTPAddr != "" ||
		!cfg.Server.MCPStdio ||
		cfg.Server.LogLevel != "debug" ||
		cfg.Server.ShutdownTimeout != 20*time.Second ||
		cfg.Server.Umask != "0007" {
		t.Fatalf("Server = %+v", cfg.Server)
	}
	if !reflect.DeepEqual(cfg.Server.RESTCORSOrigins, []string{"https://kura.example"}) {
		t.Fatalf("RESTCORSOrigins = %v", cfg.Server.RESTCORSOrigins)
	}
	if cfg.Library.Root != "/media/anime" ||
		cfg.Library.Inbox != "/media/inbox" ||
		cfg.Library.AiringTailDays != 14 {
		t.Fatalf("Library = %+v", cfg.Library)
	}
	if !reflect.DeepEqual(cfg.Metadata.PreferredLanguages, []string{"en-US", "ja"}) {
		t.Fatalf("PreferredLanguages = %v", cfg.Metadata.PreferredLanguages)
	}
	if cfg.Metadata.MediaInfoCommand != "/usr/local/bin/mediainfo" {
		t.Fatalf("MediaInfoCommand = %q", cfg.Metadata.MediaInfoCommand)
	}
	if cfg.Metadata.TVDBURL != "http://tvdb.test" {
		t.Fatalf("TVDBURL = %q", cfg.Metadata.TVDBURL)
	}
	if !cfg.Auth.Disabled || cfg.Auth.TokenPath != "/run/secrets/kura-token" {
		t.Fatalf("Auth = %+v", cfg.Auth)
	}
	if cfg.Jobs.Timeout != time.Hour ||
		cfg.Jobs.Retention != 45*time.Minute ||
		cfg.Jobs.ReaperInterval != 3*time.Minute {
		t.Fatalf("Jobs = %+v", cfg.Jobs)
	}
	if cfg.Index.ProbeInterval != 5*time.Second ||
		cfg.Index.RebuildInterval != 2*time.Hour ||
		cfg.Index.RootDebounce != 4*time.Second {
		t.Fatalf("Index = %+v", cfg.Index)
	}
	if cfg.Sweep.Interval != 30*time.Minute || cfg.Sweep.LogRetentionDays != 10 {
		t.Fatalf("Sweep = %+v", cfg.Sweep)
	}
	if cfg.Coordination.ConflictRetries != 2 {
		t.Fatalf("Coordination = %+v", cfg.Coordination)
	}
}

func TestLoadRuntimeRejectsUnknownField(t *testing.T) {
	_, err := Load(writeConfig(t, `
[library]
root = "/library"
inbox = "/inbox"
unexpected = true
`))
	if err == nil || !strings.Contains(err.Error(), "strict mode") {
		t.Fatalf("Load error = %v, want unknown-field error", err)
	}
}

func TestLoadRuntimeRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing library root",
			body: "[library]\ninbox = \"/inbox\"\n",
			want: "library.root is required",
		},
		{
			name: "missing inbox root",
			body: "[library]\nroot = \"/library\"\n",
			want: "library.inbox is required",
		},
		{
			name: "relative library root",
			body: "[library]\nroot = \"library\"\ninbox = \"/inbox\"\n",
			want: "library.root must be absolute",
		},
		{
			name: "invalid duration",
			body: "[library]\nroot = \"/library\"\ninbox = \"/inbox\"\n[jobs]\ntimeout = \"later\"\n",
			want: "jobs.timeout",
		},
		{
			name: "invalid log level",
			body: "[library]\nroot = \"/library\"\ninbox = \"/inbox\"\n[server]\nlog_level = \"trace\"\n",
			want: "server.log_level",
		},
		{
			name: "invalid umask",
			body: "[library]\nroot = \"/library\"\ninbox = \"/inbox\"\n[server]\numask = \"888\"\n",
			want: "server.umask",
		},
		{
			name: "invalid language",
			body: "[library]\nroot = \"/library\"\ninbox = \"/inbox\"\n[metadata]\npreferred_languages = [\"en_US\"]\n",
			want: "metadata.preferred_languages",
		},
		{
			name: "no transport",
			body: "[library]\nroot = \"/library\"\ninbox = \"/inbox\"\n[server]\nrest = \"\"\nmcp_http = \"\"\nmcp_stdio = false\n",
			want: "at least one transport",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
