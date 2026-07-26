// Package config loads and validates tape-backup runtime configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultPath = "/etc/kura/tape-backup.toml"

	defaultAddr        = ":8080"
	defaultLogLevel    = "info"
	defaultLTFSRoot    = "/mnt/ltfs"
	defaultDriveDevice = "/dev/nst0"
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

// Tape configures executor-only LTFS and drive paths.
type Tape struct {
	LTFSRoot    string
	DriveDevice string
}

// Defaults returns all non-required runtime defaults.
func Defaults() Config {
	return Config{
		Server: Server{
			Addr:     defaultAddr,
			LogLevel: defaultLogLevel,
		},
		Tape: Tape{
			LTFSRoot:    defaultLTFSRoot,
			DriveDevice: defaultDriveDevice,
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
	cfg := raw.resolve()
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
	LTFSRoot    *string `toml:"ltfs_root"`
	DriveDevice *string `toml:"drive_device"`
}

func (r fileConfig) resolve() Config {
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
	}
	return cfg
}

func setString(dst *string, src *string) {
	if src != nil {
		*dst = *src
	}
}
