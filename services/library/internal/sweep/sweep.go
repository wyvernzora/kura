// Package sweep runs a background janitor that prunes old reconcile
// plan JSONLs and persistent job logs under <library>/.kura/. The
// files are forensic — apply does not delete plan logs, the registry
// does not delete job logs — so without a retention sweep they
// accumulate forever. Server-only; the CLI never runs sweeps.
package sweep

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// Config tunes the sweep loop. Zero values fall back to the defaults
// at the top of Run.
type Config struct {
	// Interval is the period between sweeps. Default 1h.
	Interval time.Duration
	// LogRetention is the age threshold for deleting forensic JSONLs
	// (mtime), shared by reconcile plan logs and per-job history
	// logs. Default 7d. Configured via KURA_LOG_RETENTION_DAYS at
	// the serve binary boundary.
	LogRetention time.Duration
	// Registry, when non-nil, lets the sweep skip files that belong
	// to currently-running jobs. Tests pass nil.
	Registry *jobs.Registry
}

const (
	defaultInterval     = time.Hour
	defaultLogRetention = 7 * 24 * time.Hour
)

// Run blocks until ctx is cancelled, sweeping libRoot every Interval.
// Errors during a sweep are logged at warn level and never abort the
// loop. Returns nil on graceful shutdown (ctx cancellation).
func Run(ctx context.Context, libRoot string, cfg Config, log *slog.Logger) error {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.LogRetention <= 0 {
		cfg.LogRetention = defaultLogRetention
	}
	if log == nil {
		log = slog.Default()
	}

	log.Info("sweep starting",
		"interval", cfg.Interval,
		"logRetention", cfg.LogRetention,
	)
	for {
		timer := time.NewTimer(cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case now := <-timer.C:
			sweepOnce(libRoot, cfg, log, now)
		}
	}
}

// sweepOnce performs a single pass over libRoot. Exported in spirit
// for tests in the same package; the public surface is Run.
func sweepOnce(libRoot string, cfg Config, log *slog.Logger, now time.Time) {
	series, err := listSeries(libRoot)
	if err != nil {
		log.Warn("sweep list series failed", "err", err)
		return
	}
	var totalPlanDeleted, totalErrors int
	for _, ref := range series {
		deleted, errs := sweepSeries(libRoot, ref, cfg.LogRetention, now, log)
		totalPlanDeleted += deleted
		totalErrors += errs
	}
	jobsDeleted, jobsErrs := sweepJobs(libRoot, cfg.LogRetention, cfg.Registry, now, log)
	totalErrors += jobsErrs
	log.Info("sweep tick complete",
		"series", len(series),
		"deletedPlans", totalPlanDeleted,
		"deletedJobLogs", jobsDeleted,
		"errors", totalErrors,
	)
}

// sweepJobs deletes job-log JSONLs under <libRoot>/.kura/jobs/ whose
// mtime falls past ttl. Files whose IDs are still in the registry's
// running set are preserved — those goroutines hold the writer open.
func sweepJobs(libRoot string, ttl time.Duration, registry *jobs.Registry, now time.Time, log *slog.Logger) (deleted, errors int) {
	dir := paths.JobsDir(libRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0
		}
		log.Warn("sweep read jobs dir failed", "err", err)
		return 0, 1
	}
	var active map[string]struct{}
	if registry != nil {
		active = registry.ActiveIDs()
	}
	cutoff := now.Add(-ttl)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), paths.JobLogExtension) {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), paths.JobLogExtension)
		if _, running := active[id]; running {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			log.Warn("sweep stat job log failed", "file", entry.Name(), "err", err)
			errors++
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(full); err != nil {
			log.Warn("sweep delete job log failed", "file", entry.Name(), "err", err)
			errors++
			continue
		}
		log.Debug("sweep deleted job log", "file", entry.Name(), "age", now.Sub(info.ModTime()))
		deleted++
	}
	return deleted, errors
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
