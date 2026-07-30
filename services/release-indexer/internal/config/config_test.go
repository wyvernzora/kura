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
settle_window = "24h"

[sources.nyaa]
interval = "10m"
settle_window = "2d"
category = "1_2"
max_rps = 0.25
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
	if cfg.Sources.DMHY.SettleWindow != 24*time.Hour {
		t.Fatalf("DMHY settle window = %v, want 24h", cfg.Sources.DMHY.SettleWindow)
	}
	// Timeouts default per source: DMHY deep pages are slow.
	if cfg.Sources.DMHY.Timeout != defaultDMHYTimeout || cfg.Sources.DMHY.RequestTimeout != defaultDMHYRequestTimeout {
		t.Fatalf("DMHY timeouts = %+v", cfg.Sources.DMHY)
	}
	if cfg.Sources.DMHY.CacheTTL != defaultCacheTTL {
		t.Fatalf("DMHY cache TTL = %v, want %v", cfg.Sources.DMHY.CacheTTL, defaultCacheTTL)
	}
	if !cfg.Sources.Nyaa.Enabled || cfg.Sources.Nyaa.Interval != 10*time.Minute {
		t.Fatalf("Nyaa = %+v", cfg.Sources.Nyaa)
	}
	if cfg.Sources.Nyaa.Category != "1_2" || cfg.Sources.Nyaa.MaxRPS != 0.25 {
		t.Fatalf("Nyaa overrides = %+v", cfg.Sources.Nyaa)
	}
	if cfg.Sources.Nyaa.SettleWindow != 48*time.Hour {
		t.Fatalf("Nyaa settle window = %v, want 48h from 2d", cfg.Sources.Nyaa.SettleWindow)
	}
	if cfg.Sources.Nyaa.Timeout != defaultNyaaTimeout || cfg.Sources.Nyaa.RequestTimeout != defaultNyaaRequestTimeout {
		t.Fatalf("Nyaa timeouts = %+v", cfg.Sources.Nyaa)
	}
	if cfg.Sources.Nyaa.CacheTTL != defaultCacheTTL {
		t.Fatalf("Nyaa cache TTL = %v, want %v", cfg.Sources.Nyaa.CacheTTL, defaultCacheTTL)
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

func TestParseDurationDayWeekSuffixes(t *testing.T) {
	tests := []struct {
		raw  string
		want time.Duration
	}{
		{raw: "1d", want: 24 * time.Hour},
		{raw: "30d", want: 30 * 24 * time.Hour},
		{raw: "2w", want: 2 * 7 * 24 * time.Hour},
		{raw: "36h", want: 36 * time.Hour},
	}
	for _, tt := range tests {
		got, err := parseDuration("test", tt.raw)
		if err != nil {
			t.Fatalf("parseDuration(%q) error = %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("parseDuration(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
	if _, err := parseDuration("test", "5x"); err == nil {
		t.Fatal("parseDuration(5x) accepted, want error")
	}
	if _, err := parseDuration("test", "999999999999999999d"); err == nil {
		t.Fatal("parseDuration overflow accepted, want error")
	}
}

func TestParseDurationRejectsDayWeekOverflow(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "days", raw: "300000d"},
		{name: "weeks", raw: "50000w"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDuration("test", tt.raw)
			if err == nil || !strings.Contains(err.Error(), "overflows time.Duration") {
				t.Fatalf("parseDuration(%q) error = %v, want overflow error", tt.raw, err)
			}
		})
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
			name: "missing settle window",
			body: "[sources.dmhy]\ninterval = \"5m\"\n",
			want: "sources.dmhy.settle_window is required",
		},
		{
			name: "zero settle window",
			body: "[sources.nyaa]\ninterval = \"5m\"\nsettle_window = \"0s\"\n",
			want: "settle_window is required and must be > 0",
		},
		{
			name: "category must be string",
			body: "[sources.dmhy]\ninterval = \"5m\"\nsettle_window = \"24h\"\ncategory = 2\n",
			want: "cannot decode TOML integer into struct field",
		},
		{
			name: "invalid DMHY category",
			body: "[sources.dmhy]\ninterval = \"5m\"\nsettle_window = \"24h\"\ncategory = \"anime\"\n",
			want: "non-negative integer string",
		},
		{
			name: "zero rate",
			body: "[sources.nyaa]\ninterval = \"5m\"\nsettle_window = \"24h\"\nmax_rps = 0\n",
			want: "max_rps must be > 0",
		},
		{
			name: "negative rate",
			body: "[sources.nyaa]\ninterval = \"5m\"\nsettle_window = \"24h\"\nmax_rps = -1\n",
			want: "max_rps must be > 0",
		},
		{
			name: "disabled source still validates crawl fields",
			body: "[sources.nyaa]\nenabled = false\nmax_rps = 0\n",
			want: "max_rps must be > 0",
		},
		{
			name: "request timeout at or above run timeout",
			body: "[sources.nyaa]\ninterval = \"5m\"\nsettle_window = \"24h\"\ntimeout = \"30s\"\nrequest_timeout = \"30s\"\n",
			want: "must be greater than sources.nyaa.request_timeout",
		},
		{
			name: "zero request timeout",
			body: "[sources.nyaa]\ninterval = \"5m\"\nsettle_window = \"24h\"\nrequest_timeout = \"0s\"\n",
			want: "request_timeout must be > 0",
		},
		{
			name: "negative Nyaa cache TTL",
			body: "[sources.nyaa]\ninterval = \"5m\"\nsettle_window = \"24h\"\ncache_ttl = \"-1s\"\n",
			want: "sources.nyaa.cache_ttl must be >= 0",
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
