// Package config loads and validates tape-backup runtime configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultPath = "/etc/kura/tape-backup.toml"

	defaultAddr                  = ":8080"
	defaultLogLevel              = "info"
	defaultLTFSRoot              = "/mnt/ltfs"
	defaultDriveDevice           = "/dev/nst0"
	defaultFreeSpaceMargin int64 = 1 << 30
	defaultIdleTimeout           = 30 * time.Minute
	defaultFlushCadence          = 1
)

var validLogLevels = []string{"debug", "info", "warn", "error"}

// Config is the validated runtime configuration shared by both entrypoints.
type Config struct {
	Server  Server
	Library Library
	State   State
	Tape    Tape
}

// Server configures the long-lived control plane.
type Server struct {
	Addr     string
	LogLevel string
}

// Library configures read-only library access and its manager endpoint.
type Library struct {
	Root string
	URL  string
}

// State configures tape-backup-owned catalog, plan, and status storage.
type State struct {
	Root string
}

// Tape configures executor-only LTFS, drive, and execution policy.
type Tape struct {
	LTFSRoot        string
	DriveDevice     string
	FreeSpaceMargin int64
	IdleTimeout     time.Duration
	FlushCadence    int
}

// Defaults returns all non-required runtime defaults.
func Defaults() Config {
	return Config{
		Server: Server{
			Addr:     defaultAddr,
			LogLevel: defaultLogLevel,
		},
		Tape: Tape{
			LTFSRoot:        defaultLTFSRoot,
			DriveDevice:     defaultDriveDevice,
			FreeSpaceMargin: defaultFreeSpaceMargin,
			IdleTimeout:     defaultIdleTimeout,
			FlushCadence:    defaultFlushCadence,
		},
	}
}

// Load strictly decodes, resolves, and validates a TOML config file.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	var raw fileConfig
	if err := toml.NewDecoder(f).DisallowUnknownFields().Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("config: decode %s: %w", path, err)
	}
	cfg, err := raw.resolve()
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// Validate rejects invalid resolved configuration without probing runtime
// mounts or device availability.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Server.Addr) == "" {
		return fmt.Errorf("server.addr must not be empty")
	}
	if !slices.Contains(validLogLevels, c.Server.LogLevel) {
		return fmt.Errorf(
			"server.log_level %q is invalid (want one of %v)",
			c.Server.LogLevel,
			validLogLevels,
		)
	}
	if err := requiredAbsolutePath("library.root", c.Library.Root); err != nil {
		return err
	}
	if strings.TrimSpace(c.Library.URL) == "" {
		return fmt.Errorf("library.url is required")
	}
	if err := requiredAbsolutePath("state.root", c.State.Root); err != nil {
		return err
	}
	if err := requiredAbsolutePath("tape.ltfs_root", c.Tape.LTFSRoot); err != nil {
		return err
	}
	if c.Tape.FreeSpaceMargin < 0 {
		return fmt.Errorf("tape.free_space_margin must not be negative")
	}
	if c.Tape.IdleTimeout <= 0 {
		return fmt.Errorf("tape.idle_timeout must be greater than zero")
	}
	if c.Tape.FlushCadence < 1 {
		return fmt.Errorf("tape.flush_cadence must be at least 1")
	}
	if c.Tape.DriveDevice == "" {
		return nil
	}
	return requiredAbsolutePath("tape.drive_device", c.Tape.DriveDevice)
}

func requiredAbsolutePath(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("%s must be absolute", name)
	}
	return nil
}

type fileConfig struct {
	Server  *fileServer  `toml:"server"`
	Library *fileLibrary `toml:"library"`
	State   *fileState   `toml:"state"`
	Tape    *fileTape    `toml:"tape"`
}

type fileServer struct {
	Addr     *string `toml:"addr"`
	LogLevel *string `toml:"log_level"`
}

type fileLibrary struct {
	Root *string `toml:"root"`
	URL  *string `toml:"url"`
}

type fileState struct {
	Root *string `toml:"root"`
}

type fileTape struct {
	LTFSRoot        *string `toml:"ltfs_root"`
	DriveDevice     *string `toml:"drive_device"`
	FreeSpaceMargin *int64  `toml:"free_space_margin"`
	IdleTimeout     *string `toml:"idle_timeout"`
	FlushCadence    *int    `toml:"flush_cadence"`
}

func (r fileConfig) resolve() (Config, error) {
	cfg := Defaults()
	if r.Server != nil {
		setString(&cfg.Server.Addr, r.Server.Addr)
		setString(&cfg.Server.LogLevel, r.Server.LogLevel)
	}
	if r.Library != nil {
		setString(&cfg.Library.Root, r.Library.Root)
		setString(&cfg.Library.URL, r.Library.URL)
	}
	if r.State != nil {
		setString(&cfg.State.Root, r.State.Root)
	}
	if r.Tape != nil {
		setString(&cfg.Tape.LTFSRoot, r.Tape.LTFSRoot)
		setString(&cfg.Tape.DriveDevice, r.Tape.DriveDevice)
		setInt64(&cfg.Tape.FreeSpaceMargin, r.Tape.FreeSpaceMargin)
		setInt(&cfg.Tape.FlushCadence, r.Tape.FlushCadence)
		var err error
		cfg.Tape.IdleTimeout, err = optionalDuration(
			"tape.idle_timeout",
			r.Tape.IdleTimeout,
			cfg.Tape.IdleTimeout,
		)
		if err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

func setString(dst *string, src *string) {
	if src != nil {
		*dst = *src
	}
}

func setInt64(dst *int64, src *int64) {
	if src != nil {
		*dst = *src
	}
}

func setInt(dst *int, src *int) {
	if src != nil {
		*dst = *src
	}
}

func optionalDuration(name string, raw *string, def time.Duration) (time.Duration, error) {
	if raw == nil {
		return def, nil
	}
	value, err := time.ParseDuration(*raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is invalid: %w", name, *raw, err)
	}
	return value, nil
}
