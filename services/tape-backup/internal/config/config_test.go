package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[library]
root = "/media/anime/series"
url = "http://kura-library-manager:8080"

[state]
root = "/var/lib/kura/backup"
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Defaults()
	want.Library.Root = "/media/anime/series"
	want.Library.URL = "http://kura-library-manager:8080"
	want.State.Root = "/var/lib/kura/backup"
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Config =\n%+v\nwant\n%+v", cfg, want)
	}
	if cfg.Tape.FreeSpaceMargin != 1<<30 {
		t.Fatalf("Tape.FreeSpaceMargin = %d, want %d", cfg.Tape.FreeSpaceMargin, int64(1<<30))
	}
	if cfg.Tape.IdleTimeout != 30*time.Minute {
		t.Fatalf("Tape.IdleTimeout = %s, want %s", cfg.Tape.IdleTimeout, 30*time.Minute)
	}
	if cfg.Tape.FlushCadence != 1 {
		t.Fatalf("Tape.FlushCadence = %d, want 1", cfg.Tape.FlushCadence)
	}
}

func TestLoadAllFields(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
[server]
addr = "127.0.0.1:9000"
log_level = "debug"

[auth]
disabled = true
token_path = "/run/secrets/kura-tape-token"

[encryption]
key_file = "/run/secrets/kura-tape-key"

[library]
root = "/library"
url = "https://library.example"

[state]
root = "/state"

[tape]
ltfs_root = "/tape"
drive_device = "/dev/tape/by-id/drive"
free_space_margin = 2147483648
idle_timeout = "45m"
flush_cadence = 4
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Addr != "127.0.0.1:9000" || cfg.Server.LogLevel != "debug" {
		t.Fatalf("Server = %+v", cfg.Server)
	}
	if !cfg.Auth.Disabled || cfg.Auth.TokenPath != "/run/secrets/kura-tape-token" {
		t.Fatalf("Auth = %+v", cfg.Auth)
	}
	if cfg.Encryption.KeyFile != "/run/secrets/kura-tape-key" {
		t.Fatalf("Encryption = %+v", cfg.Encryption)
	}
	if cfg.Library.Root != "/library" || cfg.Library.URL != "https://library.example" {
		t.Fatalf("Library = %+v", cfg.Library)
	}
	if cfg.State.Root != "/state" {
		t.Fatalf("State = %+v", cfg.State)
	}
	if cfg.Tape.LTFSRoot != "/tape" ||
		cfg.Tape.DriveDevice != "/dev/tape/by-id/drive" ||
		cfg.Tape.FreeSpaceMargin != 2<<30 ||
		cfg.Tape.IdleTimeout != 45*time.Minute ||
		cfg.Tape.FlushCadence != 4 {
		t.Fatalf("Tape = %+v", cfg.Tape)
	}
}

func TestLoadAllowsEmptyDriveDevice(t *testing.T) {
	cfg, err := Load(writeConfig(t, validConfig()+`
[tape]
drive_device = ""
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Tape.DriveDevice != "" {
		t.Fatalf("Tape.DriveDevice = %q, want empty", cfg.Tape.DriveDevice)
	}
}

func TestLoadExample(t *testing.T) {
	path := filepath.Join("..", "..", "config.example.toml")
	if _, err := Load(path); err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "relative encryption key file",
			body: validConfig() + "\n[encryption]\nkey_file = \"tape.key\"\n",
			want: "encryption.key_file must be absolute",
		},
		{
			name: "unknown field",
			body: validConfig() + "\n[server]\naddress = \":9090\"\n",
			want: "strict mode",
		},
		{
			name: "empty server address",
			body: validConfig() + "\n[server]\naddr = \"\"\n",
			want: "server.addr",
		},
		{
			name: "invalid log level",
			body: validConfig() + "\n[server]\nlog_level = \"trace\"\n",
			want: "server.log_level",
		},
		{
			name: "empty auth token path",
			body: validConfig() + "\n[auth]\ntoken_path = \"\"\n",
			want: "auth.token_path must not be empty",
		},
		{
			name: "missing library root",
			body: "[library]\nurl = \"http://library\"\n[state]\nroot = \"/state\"\n",
			want: "library.root is required",
		},
		{
			name: "relative library root",
			body: "[library]\nroot = \"library\"\nurl = \"http://library\"\n[state]\nroot = \"/state\"\n",
			want: "library.root must be absolute",
		},
		{
			name: "missing library URL",
			body: "[library]\nroot = \"/library\"\n[state]\nroot = \"/state\"\n",
			want: "library.url is required",
		},
		{
			name: "missing state root",
			body: "[library]\nroot = \"/library\"\nurl = \"http://library\"\n",
			want: "state.root is required",
		},
		{
			name: "relative state root",
			body: "[library]\nroot = \"/library\"\nurl = \"http://library\"\n[state]\nroot = \"state\"\n",
			want: "state.root must be absolute",
		},
		{
			name: "relative LTFS root",
			body: validConfig() + "\n[tape]\nltfs_root = \"tape\"\n",
			want: "tape.ltfs_root must be absolute",
		},
		{
			name: "relative drive device",
			body: validConfig() + "\n[tape]\ndrive_device = \"dev/nst0\"\n",
			want: "tape.drive_device must be absolute",
		},
		{
			name: "negative free space margin",
			body: validConfig() + "\n[tape]\nfree_space_margin = -1\n",
			want: "tape.free_space_margin must not be negative",
		},
		{
			name: "invalid idle timeout",
			body: validConfig() + "\n[tape]\nidle_timeout = \"soon\"\n",
			want: `tape.idle_timeout "soon" is invalid`,
		},
		{
			name: "zero idle timeout",
			body: validConfig() + "\n[tape]\nidle_timeout = \"0s\"\n",
			want: "tape.idle_timeout must be greater than zero",
		},
		{
			name: "zero flush cadence",
			body: validConfig() + "\n[tape]\nflush_cadence = 0\n",
			want: "tape.flush_cadence must be at least 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.body))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateRejectsEmptyAuthTokenPathExactly(t *testing.T) {
	cfg := Defaults()
	cfg.Auth.TokenPath = ""
	cfg.Library.Root = "/library"
	cfg.Library.URL = "http://library"
	cfg.State.Root = "/state"

	err := cfg.Validate()
	if err == nil || err.Error() != "auth.token_path must not be empty" {
		t.Fatalf("Validate() error = %v, want %q", err, "auth.token_path must not be empty")
	}
}

func TestValidateRejectsZeroFlushCadenceExactly(t *testing.T) {
	cfg := Defaults()
	cfg.Library.Root = "/library"
	cfg.Library.URL = "http://library"
	cfg.State.Root = "/state"
	cfg.Tape.FlushCadence = 0

	err := cfg.Validate()
	if err == nil || err.Error() != "tape.flush_cadence must be at least 1" {
		t.Fatalf(
			"Validate() error = %v, want %q",
			err,
			"tape.flush_cadence must be at least 1",
		)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("Load() error = %v, want open error", err)
	}
}

func validConfig() string {
	return `
[library]
root = "/library"
url = "http://library"

[state]
root = "/state"
`
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tape-backup.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
