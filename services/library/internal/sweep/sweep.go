// Package sweep runs a background janitor that prunes old reconcile
// plan JSONLs under each series's .kura/reconcile/ directory. Plan
// files are forensic — apply does not delete them — so without a
// retention sweep they accumulate forever. Server-only; the CLI never
// runs sweeps.
package sweep

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// Config tunes the sweep loop. Zero values fall back to the defaults
// at the top of Run.
type Config struct {
	// Interval is the base period between sweeps. Default 1h. The
	// effective ticker interval is Interval + uniform(-Jitter,
	// +Jitter), sampled once at startup so concurrent replicas don't
	// drift into lockstep on the same wall-clock minute.
	Interval time.Duration
	// Jitter is the maximum offset applied to Interval at startup.
	// Default 5m.
	Jitter time.Duration
	// PlanTTL is the age threshold for deleting plan JSONLs (mtime).
	// Default 7d.
	PlanTTL time.Duration
}

const (
	defaultInterval = time.Hour
	defaultJitter   = 5 * time.Minute
	defaultPlanTTL  = 7 * 24 * time.Hour
)

// Run blocks until ctx is cancelled, sweeping libRoot every Interval.
// Errors during a sweep are logged at warn level and never abort the
// loop. Returns nil on graceful shutdown (ctx cancellation).
func Run(ctx context.Context, libRoot string, cfg Config, log *slog.Logger) error {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Jitter < 0 {
		cfg.Jitter = 0
	}
	if cfg.Jitter == 0 {
		cfg.Jitter = defaultJitter
	}
	if cfg.PlanTTL <= 0 {
		cfg.PlanTTL = defaultPlanTTL
	}
	if log == nil {
		log = slog.Default()
	}

	log.Info("sweep starting",
		"interval", cfg.Interval,
		"jitter", cfg.Jitter,
		"planTTL", cfg.PlanTTL,
	)
	// Re-sample the jittered wait every iteration so concurrent
	// replicas don't lock-step on a single startup-time offset.
	for {
		wait := jitteredInterval(cfg.Interval, cfg.Jitter)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case now := <-timer.C:
			sweepOnce(libRoot, cfg, log, now)
		}
	}
}

// jitteredInterval returns interval + uniform(-jitter, +jitter),
// clamped to a positive duration (Ticker rejects <= 0). With
// Jitter == 0 it degenerates to Interval.
func jitteredInterval(interval, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return interval
	}
	offset := time.Duration(rand.Int64N(int64(2*jitter)+1)) - jitter
	d := interval + offset
	if d <= 0 {
		return interval
	}
	return d
}

// sweepOnce performs a single pass over libRoot. Exported in spirit
// for tests in the same package; the public surface is Run.
func sweepOnce(libRoot string, cfg Config, log *slog.Logger, now time.Time) {
	series, err := listSeries(libRoot)
	if err != nil {
		log.Warn("sweep list series failed", "err", err)
		return
	}
	var totalDeleted, totalErrors int
	for _, ref := range series {
		deleted, errs := sweepSeries(libRoot, ref, cfg.PlanTTL, now, log)
		totalDeleted += deleted
		totalErrors += errs
	}
	log.Info("sweep tick complete",
		"series", len(series),
		"deletedPlans", totalDeleted,
		"errors", totalErrors,
	)
}

// sweepSeries deletes plan JSONLs older than ttl under the given
// series's reconcile dir. Per-file errors are logged and counted; the
// sweep continues on error.
func sweepSeries(libRoot string, ref refs.Series, ttl time.Duration, now time.Time, log *slog.Logger) (deleted, errors int) {
	planDir := paths.PlanDir(libRoot, ref)
	entries, err := readPlanDir(planDir)
	if err != nil {
		log.Warn("sweep read plan dir failed", "ref", ref.String(), "err", err)
		return 0, 1
	}
	cutoff := now.Add(-ttl)
	for _, entry := range entries {
		full := filepath.Join(planDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			log.Warn("sweep stat plan failed", "ref", ref.String(), "file", entry.Name(), "err", err)
			errors++
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(full); err != nil {
			log.Warn("sweep delete plan failed", "ref", ref.String(), "file", entry.Name(), "err", err)
			errors++
			continue
		}
		log.Debug("sweep deleted plan", "ref", ref.String(), "file", entry.Name(), "age", now.Sub(info.ModTime()))
		deleted++
	}
	return deleted, errors
}

// readDirSoftCap is the entry-count threshold at which sweep emits a
// warn-level log line per directory. Reading more than this many
// entries with f.ReadDir(-1) into memory is not a hard error — the
// .kura/reconcile/ and library-root layouts are operator-bounded —
// but past this point it suggests a runaway plan-leak or non-Kura
// directories cluttering the library root that operators should look
// at.
const readDirSoftCap = 10000

func readPlanDir(dir string) ([]os.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	// ReadDir(-1) loads the entire directory into memory. Plan files
	// are bounded by reconcile-token expiry; a normal library has
	// O(tens) per series and most are zero.
	entries, err := f.ReadDir(-1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	out := entries[:0]
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), paths.PlanExtension) {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func listSeries(libRoot string) ([]refs.Series, error) {
	f, err := os.Open(libRoot)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// ReadDir(-1) loads the entire library root into memory. Bounded
	// by series count, which is operator-known and typically O(hundreds).
	entries, err := f.ReadDir(-1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > readDirSoftCap {
		slog.Default().Warn("sweep: library root has unusually many entries",
			"libRoot", libRoot,
			"entries", len(entries),
			"softCap", readDirSoftCap,
		)
	}
	var out []refs.Series
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		ref, err := refs.ParseSeries(name)
		if err != nil {
			continue
		}
		out = append(out, ref)
	}
	return out, nil
}
