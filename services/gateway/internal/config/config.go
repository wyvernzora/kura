// Package config loads the bridge's strict TOML configuration and the two
// leaf-upstream environment variables.
//
// Leaf addresses are deliberately not in the file: Caddy interpolates the same
// two variables, so keeping one declaration per leaf is what stops the routing
// and the bridge from desyncing.
package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	// DefaultPath is where the Pod mounts the ConfigMap.
	DefaultPath = "/etc/kura/gateway.toml"

	// EnvLibraryUpstream and EnvReleasesUpstream name the leaf services.
	// Startup fails fast when either is unset — a bridge that silently
	// defaulted would answer health for a leaf it never contacted.
	EnvLibraryUpstream  = "KURA_LIBRARY_UPSTREAM"
	EnvReleasesUpstream = "KURA_RELEASES_UPSTREAM"
)

// Config is the validated runtime configuration.
type Config struct {
	Address string
	// MetricsAddress serves /metrics only. Unlike Address it may (and
	// should) bind the Pod network: exposition is read-only and the
	// NetworkPolicy split mirrors the leaves' :9090 listeners. Caddy owns
	// :9090 in this Pod, hence :9091.
	MetricsAddress   string
	RequestTimeout   time.Duration
	ComponentTimeout time.Duration
	MaxResponseBytes int64
	SessionTimeout   time.Duration

	LibraryUpstream  string
	ReleasesUpstream string
}

// Defaults mirrors the shipped gateway.toml.
func Defaults() Config {
	return Config{
		Address:          "127.0.0.1:8081",
		MetricsAddress:   ":9091",
		RequestTimeout:   30 * time.Second,
		ComponentTimeout: time.Second,
		MaxResponseBytes: 1 << 20,
		SessionTimeout:   5 * time.Minute,
	}
}

type fileConfig struct {
	Server *struct {
		Address        *string `toml:"address"`
		MetricsAddress *string `toml:"metrics_address"`
	} `toml:"server"`
	API *struct {
		RequestTimeout   *string `toml:"request_timeout"`
		ComponentTimeout *string `toml:"component_timeout"`
		MaxResponseBytes *int64  `toml:"max_response_bytes"`
	} `toml:"api"`
	MCP *struct {
		SessionTimeout *string `toml:"session_timeout"`
	} `toml:"mcp"`
}

// Load reads path (absent is not an error — the defaults are the shipped
// values) and the two upstream variables, then validates the result.
func Load(path string, getenv func(string) string) (Config, error) {
	cfg := Defaults()

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		var fc fileConfig
		md, derr := toml.Decode(string(raw), &fc)
		if derr != nil {
			return Config{}, fmt.Errorf("decode %s: %w", path, derr)
		}
		if undecoded := md.Undecoded(); len(undecoded) > 0 {
			return Config{}, fmt.Errorf("decode %s: unknown keys: %v", path, undecoded)
		}
		if err := applyFile(&cfg, fc); err != nil {
			return Config{}, fmt.Errorf("decode %s: %w", path, err)
		}
	case os.IsNotExist(err):
		// Defaults stand.
	default:
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}

	cfg.LibraryUpstream = strings.TrimSpace(getenv(EnvLibraryUpstream))
	cfg.ReleasesUpstream = strings.TrimSpace(getenv(EnvReleasesUpstream))

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyFile(cfg *Config, fc fileConfig) error {
	if fc.Server != nil {
		if fc.Server.Address != nil {
			cfg.Address = *fc.Server.Address
		}
		if fc.Server.MetricsAddress != nil {
			cfg.MetricsAddress = *fc.Server.MetricsAddress
		}
	}
	if fc.API != nil {
		for _, d := range []struct {
			raw  *string
			dst  *time.Duration
			name string
		}{
			{fc.API.RequestTimeout, &cfg.RequestTimeout, "api.request_timeout"},
			{fc.API.ComponentTimeout, &cfg.ComponentTimeout, "api.component_timeout"},
		} {
			if err := setDuration(d.raw, d.dst, d.name); err != nil {
				return err
			}
		}
		if fc.API.MaxResponseBytes != nil {
			cfg.MaxResponseBytes = *fc.API.MaxResponseBytes
		}
	}
	if fc.MCP != nil {
		if err := setDuration(fc.MCP.SessionTimeout, &cfg.SessionTimeout, "mcp.session_timeout"); err != nil {
			return err
		}
	}
	return nil
}

func setDuration(raw *string, dst *time.Duration, name string) error {
	if raw == nil {
		return nil
	}
	d, err := time.ParseDuration(*raw)
	if err != nil {
		return fmt.Errorf("%s %q is not a duration: %w", name, *raw, err)
	}
	*dst = d
	return nil
}

// Validate rejects configuration the bridge cannot run on.
func (c Config) Validate() error {
	if c.Address == "" {
		return fmt.Errorf("server.address must not be empty")
	}
	// Loopback-only is a security property, not a preference: a bridge on
	// the Pod network is an unauthenticated MCP endpoint reachable without
	// passing Pomerium.
	host, _, err := net.SplitHostPort(c.Address)
	if err != nil {
		return fmt.Errorf("server.address %q is not host:port: %w", c.Address, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("server.address %q must bind a loopback address", c.Address)
	}
	if c.MetricsAddress == "" {
		return fmt.Errorf("server.metrics_address must not be empty")
	}
	if _, _, err := net.SplitHostPort(c.MetricsAddress); err != nil {
		return fmt.Errorf("server.metrics_address %q is not host:port: %w", c.MetricsAddress, err)
	}
	for _, d := range []struct {
		v    time.Duration
		name string
	}{
		{c.RequestTimeout, "api.request_timeout"},
		{c.ComponentTimeout, "api.component_timeout"},
		{c.SessionTimeout, "mcp.session_timeout"},
	} {
		if d.v <= 0 {
			return fmt.Errorf("%s must be > 0", d.name)
		}
	}
	if c.MaxResponseBytes <= 0 {
		return fmt.Errorf("api.max_response_bytes must be > 0")
	}
	if c.LibraryUpstream == "" {
		return fmt.Errorf("%s is required", EnvLibraryUpstream)
	}
	if c.ReleasesUpstream == "" {
		return fmt.Errorf("%s is required", EnvReleasesUpstream)
	}
	return nil
}
