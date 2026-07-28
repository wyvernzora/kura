package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDatabaseURL = "postgres://localhost/releases"

func TestLoadDefaultsAndSources(t *testing.T) {
	path := writeConfig(t, `
[database]
schema = "release_data"

[sources.dmhy]
interval = "5m"

[sources.nyaa]
interval = "10m"
category = "1_2"
max_rps = 0
`)

	cfg, err := Load(path, testDatabaseURL)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != ":8080" || cfg.LogLevel != "info" || cfg.QueueMaxAttempts != 3 {
		t.Fatalf("server/queue defaults = %+v", cfg)
	}
	if cfg.DatabaseSchema != "release_data" {
		t.Fatalf("database schema = %q, want release_data", cfg.DatabaseSchema)
	}
	if !cfg.Sources.DMHY.Enabled || cfg.Sources.DMHY.Interval != 5*time.Minute {
		t.Fatalf("DMHY = %+v", cfg.Sources.DMHY)
	}
	if cfg.Sources.DMHY.Category != "2" || cfg.Sources.DMHY.URL != defaultDMHYURL {
		t.Fatalf("DMHY defaults = %+v", cfg.Sources.DMHY)
	}
	if !cfg.Sources.Nyaa.Enabled || cfg.Sources.Nyaa.Interval != 10*time.Minute {
		t.Fatalf("Nyaa = %+v", cfg.Sources.Nyaa)
	}
	if cfg.Sources.Nyaa.Category != "1_2" || cfg.Sources.Nyaa.MaxRPS != 0 {
		t.Fatalf("Nyaa overrides = %+v", cfg.Sources.Nyaa)
	}
}

func TestLoadAbsentAndDisabledSources(t *testing.T) {
	path := writeConfig(t, `
[sources.dmhy]
enabled = false
`)

	cfg, err := Load(path, testDatabaseURL)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Sources.DMHY.Enabled || cfg.Sources.Nyaa.Enabled {
		t.Fatalf("sources = %+v, want disabled", cfg.Sources)
	}
	if cfg.DatabaseSchema != "releases" {
		t.Fatalf("database schema = %q, want releases", cfg.DatabaseSchema)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "unknown field",
			body: "[server]\naddress = \":9090\"\n",
			want: "strict mode",
		},
		{
			name: "database URL in TOML",
			body: "[database]\nurl = \"postgres://localhost/releases\"\n",
			want: "strict mode",
		},
		{
			name: "invalid database schema",
			body: "[database]\nschema = \"Release-Indexer\"\n",
			want: "database.schema",
		},
		{
			name: "missing interval",
			body: "[sources.dmhy]\nenabled = true\n",
			want: "sources.dmhy.interval is required",
		},
		{
			name: "category must be string",
			body: "[sources.dmhy]\ninterval = \"5m\"\ncategory = 2\n",
			want: "cannot decode TOML integer into struct field",
		},
		{
			name: "invalid DMHY category",
			body: "[sources.dmhy]\ninterval = \"5m\"\ncategory = \"anime\"\n",
			want: "non-negative integer string",
		},
		{
			name: "negative rate",
			body: "[sources.nyaa]\ninterval = \"5m\"\nmax_rps = -1\n",
			want: "max_rps must be >= 0",
		},
		{
			name: "invalid duration",
			body: "[sources.nyaa]\ninterval = \"five minutes\"\n",
			want: "sources.nyaa.interval",
		},
		{
			name: "missing database URL",
			body: "",
			want: "KURA_RELEASES_DATABASE_URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			databaseURL := testDatabaseURL
			if tt.name == "missing database URL" {
				databaseURL = ""
			}
			_, err := Load(writeConfig(t, tt.body), databaseURL)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestValidateDatabaseSchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		{name: "default", schema: "releases"},
		{name: "underscore", schema: "release_indexer"},
		{name: "leading underscore", schema: "_releases2"},
		{name: "maximum length", schema: "a" + strings.Repeat("0", 62)},
		{name: "empty", schema: "", wantErr: true},
		{name: "uppercase", schema: "Releases", wantErr: true},
		{name: "hyphen", schema: "release-indexer", wantErr: true},
		{name: "leading digit", schema: "1releases", wantErr: true},
		{name: "too long", schema: "a" + strings.Repeat("0", 63), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults(testDatabaseURL)
			cfg.DatabaseSchema = tt.schema
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"), testDatabaseURL)
	if err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("Load() error = %v, want open error", err)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "release-indexer.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// metrics_addr is required rather than optional: an empty value falling back
// to addr would silently put /metrics back on the API listener, which is the
// exposure the separate listener exists to prevent.
func TestValidateRejectsMetricsAddrSharingTheAPIListener(t *testing.T) {
	base := Defaults("postgres://localhost/kura")

	cfg := base
	cfg.MetricsAddr = ""
	if err := cfg.Validate(); err == nil {
		t.Error("empty metrics_addr accepted, want error")
	}

	cfg = base
	cfg.MetricsAddr = cfg.Addr
	if err := cfg.Validate(); err == nil {
		t.Errorf("metrics_addr == addr (%q) accepted, want error", cfg.Addr)
	}
}
