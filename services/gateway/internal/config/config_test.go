package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

func TestLoadDefaultsWhenFileAbsent(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"), env(map[string]string{
		EnvLibraryUpstream:  "lib:8080",
		EnvReleasesUpstream: "rel:8080",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Address != "127.0.0.1:8081" || cfg.RequestTimeout != 30*time.Second {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte("[server]\naddres = \"127.0.0.1:8081\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, env(map[string]string{
		EnvLibraryUpstream:  "lib:8080",
		EnvReleasesUpstream: "rel:8080",
	}))
	if err == nil {
		t.Fatal("typo'd key accepted, want error")
	}
}

func TestLoadRejectsRetiredProviderTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte("[api]\nprovider_timeout = \"60s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path, env(map[string]string{
		EnvLibraryUpstream:  "lib:8080",
		EnvReleasesUpstream: "rel:8080",
	}))
	if err == nil {
		t.Fatal("retired api.provider_timeout accepted, want unknown-key error")
	}
	if !strings.Contains(err.Error(), "api.provider_timeout") {
		t.Fatalf("error = %q, want api.provider_timeout", err)
	}
}

// Both upstreams are required: a bridge that defaulted one would report health
// for a leaf it never contacted.
func TestLoadRequiresBothUpstreams(t *testing.T) {
	for _, missing := range []string{EnvLibraryUpstream, EnvReleasesUpstream} {
		vars := map[string]string{EnvLibraryUpstream: "lib:8080", EnvReleasesUpstream: "rel:8080"}
		delete(vars, missing)
		if _, err := Load(filepath.Join(t.TempDir(), "nope.toml"), env(vars)); err == nil {
			t.Errorf("missing %s accepted, want error", missing)
		}
	}
}

// Binding off-loopback would publish an unauthenticated MCP endpoint reachable
// without passing Pomerium.
func TestValidateRequiresLoopback(t *testing.T) {
	base := Defaults()
	base.LibraryUpstream = "lib:8080"
	base.ReleasesUpstream = "rel:8080"

	for _, addr := range []string{"0.0.0.0:8081", ":8081", "10.4.2.9:8081"} {
		cfg := base
		cfg.Address = addr
		if err := cfg.Validate(); err == nil {
			t.Errorf("address %q accepted, want rejection", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:8081", "[::1]:8081"} {
		cfg := base
		cfg.Address = addr
		if err := cfg.Validate(); err != nil {
			t.Errorf("loopback %q rejected: %v", addr, err)
		}
	}
}

func TestMetricsAddressDefaultAndOverride(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"), env(map[string]string{
		EnvLibraryUpstream:  "lib:8080",
		EnvReleasesUpstream: "rel:8080",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MetricsAddress != ":9091" {
		t.Fatalf("default metrics_address = %q, want :9091", cfg.MetricsAddress)
	}

	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte("[server]\nmetrics_address = \":9191\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path, env(map[string]string{
		EnvLibraryUpstream:  "lib:8080",
		EnvReleasesUpstream: "rel:8080",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MetricsAddress != ":9191" {
		t.Fatalf("metrics_address = %q, want :9191", cfg.MetricsAddress)
	}
}

// The session timeout is the only reclaim path for MCP sessions — n8n's
// client never sends DELETE — and it must outlive the longest gap between a
// client's tool calls. A ConfigMap-mounted gateway.toml is how a deployment
// retunes it, so the file has to win over the baked default.
func TestSessionTimeoutDefaultAndFileOverride(t *testing.T) {
	vars := map[string]string{EnvLibraryUpstream: "lib:8080", EnvReleasesUpstream: "rel:8080"}

	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"), env(vars))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionTimeout != 2*time.Hour {
		t.Fatalf("default session_timeout = %s, want 2h", cfg.SessionTimeout)
	}

	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte("[mcp]\nsession_timeout = \"45m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path, env(vars))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SessionTimeout != 45*time.Minute {
		t.Fatalf("session_timeout = %s, want 45m", cfg.SessionTimeout)
	}
}

// A ConfigMap typo must fail startup rather than silently fall back to a
// value the operator did not choose.
func TestLoadRejectsUnusableSessionTimeout(t *testing.T) {
	for name, body := range map[string]string{
		"not a duration": "[mcp]\nsession_timeout = \"forever\"\n",
		"non-positive":   "[mcp]\nsession_timeout = \"0s\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gateway.toml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path, env(map[string]string{
				EnvLibraryUpstream:  "lib:8080",
				EnvReleasesUpstream: "rel:8080",
			}))
			if err == nil {
				t.Fatal("unusable session_timeout accepted, want error")
			}
			if !strings.Contains(err.Error(), "session_timeout") {
				t.Fatalf("error = %q, want it to name session_timeout", err)
			}
		})
	}
}

func TestValidateRejectsMetricsAddressCollision(t *testing.T) {
	for name, metrics := range map[string]string{
		"identical": "127.0.0.1:8081",
		"wildcard":  ":8081",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := Defaults()
			cfg.MetricsAddress = metrics
			cfg.LibraryUpstream = "lib:8080"
			cfg.ReleasesUpstream = "rel:8080"
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), "collides") {
				t.Fatalf("Validate = %v, want collision error", err)
			}
		})
	}
}
