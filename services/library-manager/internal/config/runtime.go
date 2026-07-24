// Package config loads and validates library-manager runtime configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	defaultRESTAddr             = ":8080"
	defaultMCPHTTPAddr          = ":8081"
	defaultLogLevel             = "info"
	defaultShutdownTimeout      = 10 * time.Second
	defaultMediaInfoCommand     = "mediainfo"
	defaultAiringTailDays       = 7
	defaultTokenPath            = "/var/lib/kura/token"
	defaultJobRetention         = 30 * time.Minute
	defaultJobReaperInterval    = 5 * time.Minute
	defaultIndexProbeInterval   = 2 * time.Second
	defaultIndexRebuildInterval = time.Hour
	defaultIndexRootDebounce    = 3 * time.Second
	defaultSweepInterval        = time.Hour
	defaultLogRetentionDays     = 7
	defaultConflictRetries      = 1
)

var validLogLevels = []string{"debug", "info", "warn", "error"}

// Config is the validated runtime configuration for kura serve.
type Config struct {
	Server       Server
	Library      Library
	Metadata     Metadata
	Auth         Auth
	Jobs         Jobs
	Index        Index
	Sweep        Sweep
	Coordination Coordination
}

// Server configures transports and process-wide behavior.
type Server struct {
	RESTAddr        string
	MCPHTTPAddr     string
	MCPStdio        bool
	RESTCORSOrigins []string
	RESTPortFile    string
	LogLevel        string
	ShutdownTimeout time.Duration
	Umask           string
}

// Library configures the managed library and inbox roots.
type Library struct {
	Root           string
	Inbox          string
	AiringTailDays int
}

// Metadata configures local media inspection and title preferences.
type Metadata struct {
	PreferredLanguages []string
	MediaInfoCommand   string
	TVDBURL            string
}

// Auth configures the shared bearer-token gate. The token itself remains
// outside TOML and is supplied through KURA_TOKEN or the token file.
type Auth struct {
	Disabled  bool
	TokenPath string
}

// Jobs configures asynchronous workflow jobs.
type Jobs struct {
	Timeout        time.Duration
	Retention      time.Duration
	ReaperInterval time.Duration
}

// Index configures the background library-index watcher.
type Index struct {
	ProbeInterval   time.Duration
	RebuildInterval time.Duration
	RootDebounce    time.Duration
}

// Sweep configures forensic-log pruning.
type Sweep struct {
	Interval         time.Duration
	LogRetentionDays int
}

// Coordination configures optimistic-concurrency retries.
type Coordination struct {
	ConflictRetries int
}

// Defaults returns all non-required runtime defaults.
func Defaults() Config {
	return Config{
		Server: Server{
			RESTAddr:        defaultRESTAddr,
			MCPHTTPAddr:     defaultMCPHTTPAddr,
			LogLevel:        defaultLogLevel,
			ShutdownTimeout: defaultShutdownTimeout,
		},
		Library: Library{
			AiringTailDays: defaultAiringTailDays,
		},
		Metadata: Metadata{
			MediaInfoCommand: defaultMediaInfoCommand,
		},
		Auth: Auth{
			TokenPath: defaultTokenPath,
		},
		Jobs: Jobs{
			Retention:      defaultJobRetention,
			ReaperInterval: defaultJobReaperInterval,
		},
		Index: Index{
			ProbeInterval:   defaultIndexProbeInterval,
			RebuildInterval: defaultIndexRebuildInterval,
			RootDebounce:    defaultIndexRootDebounce,
		},
		Sweep: Sweep{
			Interval:         defaultSweepInterval,
			LogRetentionDays: defaultLogRetentionDays,
		},
		Coordination: Coordination{
			ConflictRetries: defaultConflictRetries,
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
	return cfg, nil
}

// Validate rejects an invalid resolved configuration.
func (c Config) Validate() error {
	validators := []func() error{
		c.Server.validate,
		c.Library.validate,
		c.Metadata.validate,
		c.Auth.validate,
		c.Jobs.validate,
		c.Index.validate,
		c.Sweep.validate,
		c.Coordination.validate,
	}
	for _, validate := range validators {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c Server) validate() error {
	if c.RESTAddr == "" && c.MCPHTTPAddr == "" && !c.MCPStdio {
		return fmt.Errorf("server must enable at least one transport")
	}
	if !slices.Contains(validLogLevels, c.LogLevel) {
		return fmt.Errorf("server.log_level %q is invalid (want one of %v)", c.LogLevel, validLogLevels)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("server.shutdown_timeout must be > 0")
	}
	return validateUmask(c.Umask)
}

func (c Library) validate() error {
	if strings.TrimSpace(c.Root) == "" {
		return fmt.Errorf("library.root is required")
	}
	if !filepath.IsAbs(c.Root) {
		return fmt.Errorf("library.root must be absolute")
	}
	if strings.TrimSpace(c.Inbox) == "" {
		return fmt.Errorf("library.inbox is required")
	}
	if !filepath.IsAbs(c.Inbox) {
		return fmt.Errorf("library.inbox must be absolute")
	}
	if c.AiringTailDays < 0 {
		return fmt.Errorf("library.airing_tail_days must be >= 0")
	}
	return nil
}

func (c Metadata) validate() error {
	if strings.TrimSpace(c.MediaInfoCommand) == "" {
		return fmt.Errorf("metadata.mediainfo_command must not be empty")
	}
	return nil
}

func (c Auth) validate() error {
	if strings.TrimSpace(c.TokenPath) == "" {
		return fmt.Errorf("auth.token_path must not be empty")
	}
	return nil
}

func (c Jobs) validate() error {
	if c.Timeout < 0 {
		return fmt.Errorf("jobs.timeout must be >= 0")
	}
	if c.Retention < 0 {
		return fmt.Errorf("jobs.retention must be >= 0")
	}
	if c.ReaperInterval < 0 {
		return fmt.Errorf("jobs.reaper_interval must be >= 0")
	}
	return nil
}

func (c Index) validate() error {
	if c.ProbeInterval < 0 {
		return fmt.Errorf("index.probe_interval must be >= 0")
	}
	if c.RebuildInterval < 0 {
		return fmt.Errorf("index.rebuild_interval must be >= 0")
	}
	if c.RootDebounce < 0 {
		return fmt.Errorf("index.library_root_debounce must be >= 0")
	}
	return nil
}

func (c Sweep) validate() error {
	if c.Interval <= 0 {
		return fmt.Errorf("sweep.interval must be > 0")
	}
	if c.LogRetentionDays <= 0 {
		return fmt.Errorf("sweep.log_retention_days must be > 0")
	}
	return nil
}

func (c Coordination) validate() error {
	if c.ConflictRetries < 0 {
		return fmt.Errorf("coordination.conflict_retries must be >= 0")
	}
	return nil
}

func validateUmask(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseUint(raw, 8, 12)
	if err != nil || parsed > 0o777 {
		return fmt.Errorf("server.umask must be an octal mode between 0000 and 0777")
	}
	return nil
}

type fileConfig struct {
	Server       *fileServer       `toml:"server"`
	Library      *fileLibrary      `toml:"library"`
	Metadata     *fileMetadata     `toml:"metadata"`
	Auth         *fileAuth         `toml:"auth"`
	Jobs         *fileJobs         `toml:"jobs"`
	Index        *fileIndex        `toml:"index"`
	Sweep        *fileSweep        `toml:"sweep"`
	Coordination *fileCoordination `toml:"coordination"`
}

type fileServer struct {
	RESTAddr        *string  `toml:"rest"`
	MCPHTTPAddr     *string  `toml:"mcp_http"`
	MCPStdio        *bool    `toml:"mcp_stdio"`
	RESTCORSOrigins []string `toml:"rest_cors_origins"`
	RESTPortFile    *string  `toml:"rest_port_file"`
	LogLevel        *string  `toml:"log_level"`
	ShutdownTimeout *string  `toml:"shutdown_timeout"`
	Umask           *string  `toml:"umask"`
}

type fileLibrary struct {
	Root           *string `toml:"root"`
	Inbox          *string `toml:"inbox"`
	AiringTailDays *int    `toml:"airing_tail_days"`
}

type fileMetadata struct {
	PreferredLanguages []string `toml:"preferred_languages"`
	MediaInfoCommand   *string  `toml:"mediainfo_command"`
	TVDBURL            *string  `toml:"tvdb_url"`
}

type fileAuth struct {
	Disabled  *bool   `toml:"disabled"`
	TokenPath *string `toml:"token_path"`
}

type fileJobs struct {
	Timeout        *string `toml:"timeout"`
	Retention      *string `toml:"retention"`
	ReaperInterval *string `toml:"reaper_interval"`
}

type fileIndex struct {
	ProbeInterval   *string `toml:"probe_interval"`
	RebuildInterval *string `toml:"rebuild_interval"`
	RootDebounce    *string `toml:"library_root_debounce"`
}

type fileSweep struct {
	Interval         *string `toml:"interval"`
	LogRetentionDays *int    `toml:"log_retention_days"`
}

type fileCoordination struct {
	ConflictRetries *int `toml:"conflict_retries"`
}

func (r fileConfig) resolve() (Config, error) {
	cfg := Defaults()
	if err := r.Server.resolve(&cfg.Server); err != nil {
		return Config{}, err
	}
	r.Library.resolve(&cfg.Library)
	if err := r.Metadata.resolve(&cfg.Metadata); err != nil {
		return Config{}, err
	}
	r.Auth.resolve(&cfg.Auth)
	if err := r.Jobs.resolve(&cfg.Jobs); err != nil {
		return Config{}, err
	}
	if err := r.Index.resolve(&cfg.Index); err != nil {
		return Config{}, err
	}
	if err := r.Sweep.resolve(&cfg.Sweep); err != nil {
		return Config{}, err
	}
	r.Coordination.resolve(&cfg.Coordination)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (r *fileServer) resolve(dst *Server) error {
	if r == nil {
		return nil
	}
	setString(&dst.RESTAddr, r.RESTAddr)
	setString(&dst.MCPHTTPAddr, r.MCPHTTPAddr)
	setBool(&dst.MCPStdio, r.MCPStdio)
	if r.RESTCORSOrigins != nil {
		dst.RESTCORSOrigins = slices.Clone(r.RESTCORSOrigins)
	}
	setString(&dst.RESTPortFile, r.RESTPortFile)
	setString(&dst.LogLevel, r.LogLevel)
	setString(&dst.Umask, r.Umask)
	var err error
	dst.ShutdownTimeout, err = optionalDuration(
		"server.shutdown_timeout",
		r.ShutdownTimeout,
		dst.ShutdownTimeout,
	)
	return err
}

func (r *fileLibrary) resolve(dst *Library) {
	if r == nil {
		return
	}
	setString(&dst.Root, r.Root)
	setString(&dst.Inbox, r.Inbox)
	setInt(&dst.AiringTailDays, r.AiringTailDays)
}

func (r *fileMetadata) resolve(dst *Metadata) error {
	if r == nil {
		return nil
	}
	if r.PreferredLanguages != nil {
		prefs, err := ParsePreferredLanguages(strings.Join(r.PreferredLanguages, ","))
		if err != nil {
			return fmt.Errorf("metadata.preferred_languages: %w", err)
		}
		dst.PreferredLanguages = prefs.Tags()
	}
	setString(&dst.MediaInfoCommand, r.MediaInfoCommand)
	setString(&dst.TVDBURL, r.TVDBURL)
	return nil
}

func (r *fileAuth) resolve(dst *Auth) {
	if r == nil {
		return
	}
	setBool(&dst.Disabled, r.Disabled)
	setString(&dst.TokenPath, r.TokenPath)
}

func (r *fileJobs) resolve(dst *Jobs) error {
	if r == nil {
		return nil
	}
	var err error
	if dst.Timeout, err = optionalDuration("jobs.timeout", r.Timeout, dst.Timeout); err != nil {
		return err
	}
	if dst.Retention, err = optionalDuration("jobs.retention", r.Retention, dst.Retention); err != nil {
		return err
	}
	dst.ReaperInterval, err = optionalDuration(
		"jobs.reaper_interval",
		r.ReaperInterval,
		dst.ReaperInterval,
	)
	return err
}

func (r *fileIndex) resolve(dst *Index) error {
	if r == nil {
		return nil
	}
	var err error
	if dst.ProbeInterval, err = optionalDuration("index.probe_interval", r.ProbeInterval, dst.ProbeInterval); err != nil {
		return err
	}
	if dst.RebuildInterval, err = optionalDuration(
		"index.rebuild_interval",
		r.RebuildInterval,
		dst.RebuildInterval,
	); err != nil {
		return err
	}
	dst.RootDebounce, err = optionalDuration(
		"index.library_root_debounce",
		r.RootDebounce,
		dst.RootDebounce,
	)
	return err
}

func (r *fileSweep) resolve(dst *Sweep) error {
	if r == nil {
		return nil
	}
	var err error
	dst.Interval, err = optionalDuration("sweep.interval", r.Interval, dst.Interval)
	setInt(&dst.LogRetentionDays, r.LogRetentionDays)
	return err
}

func (r *fileCoordination) resolve(dst *Coordination) {
	if r == nil {
		return
	}
	setInt(&dst.ConflictRetries, r.ConflictRetries)
}

func optionalDuration(name string, raw *string, def time.Duration) (time.Duration, error) {
	if raw == nil {
		return def, nil
	}
	d, err := time.ParseDuration(*raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is invalid: %w", name, *raw, err)
	}
	return d, nil
}

func setString(dst *string, src *string) {
	if src != nil {
		*dst = *src
	}
}

func setBool(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

func setInt(dst *int, src *int) {
	if src != nil {
		*dst = *src
	}
}
