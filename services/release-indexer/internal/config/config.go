// Package config loads and validates release-indexer runtime configuration.
package config

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	DefaultPath = "/etc/kura/release-indexer.toml"

	defaultAddr             = ":8080"
	defaultMetricsAddr      = ":9090"
	defaultDatabaseSchema   = "releases"
	defaultLogLevel         = "info"
	defaultQueueMaxAttempts = 3
	defaultTimeout          = 2 * time.Minute
	defaultMaxRPS           = 0.5
	defaultDMHYURL          = "https://share.dmhy.org"
	defaultDMHYCategory     = "2"
	defaultDMHYCacheTTL     = 10 * time.Minute
	defaultNyaaURL          = "https://nyaa.si"
	defaultNyaaCategory     = "1_4"
	defaultNyaaFilter       = "0"
)

var (
	validDatabaseSchema = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)
	validLogLevels      = []string{"debug", "info", "warn", "error"}
)

// Config is the validated runtime configuration.
type Config struct {
	Addr string
	// MetricsAddr is a separate listener for /metrics only. It must never
	// equal Addr: a NetworkPolicy that lets Prometheus scrape metrics would
	// otherwise also hand it the unauthenticated write API.
	MetricsAddr      string
	DatabaseSchema   string
	DatabaseURL      string
	LogLevel         string
	QueueMaxAttempts int
	Sources          Sources
}

type Sources struct {
	DMHY SourceDMHY
	Nyaa SourceNyaa
}

type SourceDMHY struct {
	Enabled  bool
	Interval time.Duration
	Timeout  time.Duration
	URL      string
	Category string
	MaxRPS   float64
	CacheTTL time.Duration
}

type SourceNyaa struct {
	Enabled  bool
	Interval time.Duration
	Timeout  time.Duration
	URL      string
	Query    string
	Category string
	Filter   string
	MaxRPS   float64
}

// Defaults returns the non-secret defaults with all sources disabled.
func Defaults(databaseURL string) Config {
	return Config{
		Addr:             defaultAddr,
		MetricsAddr:      defaultMetricsAddr,
		DatabaseSchema:   defaultDatabaseSchema,
		DatabaseURL:      databaseURL,
		LogLevel:         defaultLogLevel,
		QueueMaxAttempts: defaultQueueMaxAttempts,
		Sources: Sources{
			DMHY: SourceDMHY{
				Timeout:  defaultTimeout,
				URL:      defaultDMHYURL,
				Category: defaultDMHYCategory,
				MaxRPS:   defaultMaxRPS,
				CacheTTL: defaultDMHYCacheTTL,
			},
			Nyaa: SourceNyaa{
				Timeout:  defaultTimeout,
				URL:      defaultNyaaURL,
				Category: defaultNyaaCategory,
				Filter:   defaultNyaaFilter,
				MaxRPS:   defaultMaxRPS,
			},
		},
	}
}

// Load decodes a strict TOML file, applies documented defaults, and validates
// the result. DatabaseURL remains outside TOML so deployments can inject it
// from a Secret.
func Load(path, databaseURL string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	var raw fileConfig
	if err := toml.NewDecoder(f).DisallowUnknownFields().Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("config: decode %s: %w", path, err)
	}

	cfg, err := raw.resolve(databaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// Validate rejects invalid resolved configuration.
func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("database URL is required in KURA_RELEASES_DATABASE_URL")
	}
	if !validDatabaseSchema.MatchString(c.DatabaseSchema) {
		return fmt.Errorf("database.schema %q is invalid (must match %s)", c.DatabaseSchema, validDatabaseSchema)
	}
	if c.Addr == "" {
		return fmt.Errorf("server.addr must not be empty")
	}
	// Required rather than optional on purpose: an empty value that fell
	// back to Addr would silently reintroduce the exposure this split
	// exists to prevent.
	if c.MetricsAddr == "" {
		return fmt.Errorf("server.metrics_addr must not be empty")
	}
	if addrsCollide(c.MetricsAddr, c.Addr) {
		return fmt.Errorf("server.metrics_addr %q collides with server.addr %q", c.MetricsAddr, c.Addr)
	}
	if !slices.Contains(validLogLevels, c.LogLevel) {
		return fmt.Errorf("server.log_level %q is invalid (want one of %v)", c.LogLevel, validLogLevels)
	}
	if c.QueueMaxAttempts <= 0 {
		return fmt.Errorf("queue.max_attempts must be > 0")
	}
	if err := validateDMHY(c.Sources.DMHY); err != nil {
		return err
	}
	if err := validateNyaa(c.Sources.Nyaa); err != nil {
		return err
	}
	return nil
}

// addrsCollide reports whether two listen addresses would contend for
// the same socket: same non-zero port with equal hosts, or either side
// binding the wildcard. Mirrors the library-manager and gateway config
// checks so all three dual-listener services fail the same way.
//
// Deliberately best-effort and textual: it catches the realistic
// footgun (same or default port twice) with a clear config error;
// exotic spellings and mixed-family nuances are left to net.Listen's
// bind-time failure, which still aborts startup fast.
func addrsCollide(a, b string) bool {
	ah, ap, aerr := net.SplitHostPort(a)
	bh, bp, berr := net.SplitHostPort(b)
	if aerr != nil || berr != nil {
		return a == b
	}
	if ap == "0" || bp == "0" || ap != bp {
		return false
	}
	if ah == bh {
		return true
	}
	wild := func(h string) bool { return h == "" || h == "0.0.0.0" || h == "::" }
	return wild(ah) || wild(bh)
}

func validateDMHY(c SourceDMHY) error {
	if !c.Enabled {
		return nil
	}
	if err := validateSource("sources.dmhy", c.Interval, c.Timeout, c.URL, c.MaxRPS); err != nil {
		return err
	}
	category, err := strconv.Atoi(c.Category)
	if err != nil || category < 0 {
		return fmt.Errorf("sources.dmhy.category must be a non-negative integer string")
	}
	if c.CacheTTL < 0 {
		return fmt.Errorf("sources.dmhy.cache_ttl must be >= 0")
	}
	return nil
}

func validateNyaa(c SourceNyaa) error {
	if !c.Enabled {
		return nil
	}
	return validateSource("sources.nyaa", c.Interval, c.Timeout, c.URL, c.MaxRPS)
}

func validateSource(name string, interval, timeout time.Duration, url string, maxRPS float64) error {
	if interval <= 0 {
		return fmt.Errorf("%s.interval is required and must be > 0", name)
	}
	if timeout <= 0 {
		return fmt.Errorf("%s.timeout must be > 0", name)
	}
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("%s.url must not be empty", name)
	}
	if maxRPS < 0 {
		return fmt.Errorf("%s.max_rps must be >= 0", name)
	}
	return nil
}

type fileConfig struct {
	Database *fileDatabase `toml:"database"`
	Server   *fileServer   `toml:"server"`
	Queue    *fileQueue    `toml:"queue"`
	Sources  *fileSources  `toml:"sources"`
}

type fileDatabase struct {
	Schema *string `toml:"schema"`
}

type fileServer struct {
	Addr        *string `toml:"addr"`
	MetricsAddr *string `toml:"metrics_addr"`
	LogLevel    *string `toml:"log_level"`
}

type fileQueue struct {
	MaxAttempts *int `toml:"max_attempts"`
}

type fileSources struct {
	DMHY *fileDMHY `toml:"dmhy"`
	Nyaa *fileNyaa `toml:"nyaa"`
}

type fileDMHY struct {
	Enabled  *bool    `toml:"enabled"`
	Interval *string  `toml:"interval"`
	Timeout  *string  `toml:"timeout"`
	URL      *string  `toml:"url"`
	Category *string  `toml:"category"`
	MaxRPS   *float64 `toml:"max_rps"`
	CacheTTL *string  `toml:"cache_ttl"`
}

type fileNyaa struct {
	Enabled  *bool    `toml:"enabled"`
	Interval *string  `toml:"interval"`
	Timeout  *string  `toml:"timeout"`
	URL      *string  `toml:"url"`
	Query    *string  `toml:"query"`
	Category *string  `toml:"category"`
	Filter   *string  `toml:"filter"`
	MaxRPS   *float64 `toml:"max_rps"`
}

func (r fileConfig) resolve(databaseURL string) (Config, error) {
	cfg := Defaults(databaseURL)
	if r.Database != nil {
		setString(&cfg.DatabaseSchema, r.Database.Schema)
	}
	if r.Server != nil {
		setString(&cfg.Addr, r.Server.Addr)
		setString(&cfg.MetricsAddr, r.Server.MetricsAddr)
		setString(&cfg.LogLevel, r.Server.LogLevel)
	}
	if r.Queue != nil && r.Queue.MaxAttempts != nil {
		cfg.QueueMaxAttempts = *r.Queue.MaxAttempts
	}
	if r.Sources != nil && r.Sources.DMHY != nil {
		if err := resolveDMHY(&cfg.Sources.DMHY, r.Sources.DMHY); err != nil {
			return Config{}, err
		}
	}
	if r.Sources != nil && r.Sources.Nyaa != nil {
		if err := resolveNyaa(&cfg.Sources.Nyaa, r.Sources.Nyaa); err != nil {
			return Config{}, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func resolveDMHY(dst *SourceDMHY, src *fileDMHY) error {
	dst.Enabled = true
	setBool(&dst.Enabled, src.Enabled)
	setString(&dst.URL, src.URL)
	setString(&dst.Category, src.Category)
	setFloat(&dst.MaxRPS, src.MaxRPS)

	var err error
	if dst.Interval, err = requiredDuration("sources.dmhy.interval", src.Interval, dst.Enabled); err != nil {
		return err
	}
	if dst.Timeout, err = optionalDuration("sources.dmhy.timeout", src.Timeout, dst.Timeout); err != nil {
		return err
	}
	if dst.CacheTTL, err = optionalDuration("sources.dmhy.cache_ttl", src.CacheTTL, dst.CacheTTL); err != nil {
		return err
	}
	return nil
}

func resolveNyaa(dst *SourceNyaa, src *fileNyaa) error {
	dst.Enabled = true
	setBool(&dst.Enabled, src.Enabled)
	setString(&dst.URL, src.URL)
	setString(&dst.Query, src.Query)
	setString(&dst.Category, src.Category)
	setString(&dst.Filter, src.Filter)
	setFloat(&dst.MaxRPS, src.MaxRPS)

	var err error
	if dst.Interval, err = requiredDuration("sources.nyaa.interval", src.Interval, dst.Enabled); err != nil {
		return err
	}
	if dst.Timeout, err = optionalDuration("sources.nyaa.timeout", src.Timeout, dst.Timeout); err != nil {
		return err
	}
	return nil
}

func requiredDuration(name string, raw *string, required bool) (time.Duration, error) {
	if raw == nil {
		if required {
			return 0, fmt.Errorf("%s is required when the source is enabled", name)
		}
		return 0, nil
	}
	return parseDuration(name, *raw)
}

func optionalDuration(name string, raw *string, def time.Duration) (time.Duration, error) {
	if raw == nil {
		return def, nil
	}
	return parseDuration(name, *raw)
}

func parseDuration(name, raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s %q is invalid: %w", name, raw, err)
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

func setFloat(dst *float64, src *float64) {
	if src != nil {
		*dst = *src
	}
}
