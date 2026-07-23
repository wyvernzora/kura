package workflow

import (
	"context"
	"errors"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/wyvernzora/kura/services/library/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library/internal/errkind"
	"github.com/wyvernzora/kura/services/library/internal/jobs"
	"github.com/wyvernzora/kura/services/library/internal/progress"
	"github.com/wyvernzora/kura/services/library/internal/response"
	"github.com/wyvernzora/kura/services/library/internal/storage/indexfile"
)

// defaultScanAllConcurrency caps fan-out when ScanAllInput.Concurrency
// is unset. Matches the CLI's `kura scan --all` default — comfortable
// margin under TVDB's unwritten RPS ceiling while still parallelizing
// the bulk of wall-clock on a typical library.
const defaultScanAllConcurrency = 4

// ScanAllInput parameters for the library-wide ScanAll workflow.
// Forwarded to each per-series scan; identical semantics to ScanInput
// fields with the same names.
type ScanAllInput struct {
	Refresh      bool
	MetadataOnly bool
	// Concurrency caps the worker pool. Zero falls back to
	// defaultScanAllConcurrency.
	Concurrency int
}

// ScanAll dispatches a per-series scan against every tracked
// (non-untracked) row in the library and reports aggregate progress
// as a single tracked job.
//
// Keyed on refs.Series{} (zero series): the registry's cross-kind
// busy rejection naturally enforces "one library-wide job at a time"
// — submitting ScanAll while Reindex is running (or vice versa)
// returns *JobBusyError without spawning.
//
// Per-series failures do NOT terminate the job. The job succeeds with
// a ScanAllResult that tallies succeeded/failed and carries
// per-failure detail. The job only terminal-fails on infrastructure
// errors (index unavailable, ctx cancelled).
//
// Includes rows in error status — re-scan is a fix path for several
// error classes, so skipping them defeats the purpose. Only untracked
// rows are skipped.
func ScanAll(ctx context.Context, deps Deps, in ScanAllInput) *jobs.Job[response.ScanAllResult] {
	return jobs.Submit(deps.Jobs, ctx, jobs.KindScanAll, refs.Series{}, func(jobCtx context.Context) (response.ScanAllResult, error) {
		return runScanAll(jobCtx, deps, in)
	})
}

func runScanAll(ctx context.Context, deps Deps, in ScanAllInput) (response.ScanAllResult, error) {
	if deps.Index == nil {
		return response.ScanAllResult{}, errors.New("scan_all: index not available")
	}
	rows, err := deps.Index.Snapshot()
	if errors.Is(err, indexfile.ErrNotReady) {
		return response.ScanAllResult{}, &ServerNotReadyError{Reason: "library index is rebuilding"}
	}
	if err != nil {
		return response.ScanAllResult{}, err
	}
	targets := make([]refs.Series, 0, len(rows))
	for _, row := range rows {
		if row.Status == response.ListStatusUntracked {
			continue
		}
		targets = append(targets, row.Series)
	}
	total := len(targets)
	progress.Start(ctx, "scan_all", "", total)

	if total == 0 {
		progress.Success(ctx, "scan_all", "", 0)
		return response.ScanAllResult{Total: 0}, nil
	}

	workers := in.Concurrency
	if workers <= 0 {
		workers = defaultScanAllConcurrency
	}
	if workers > total {
		workers = total
	}

	var (
		mu        sync.Mutex
		done      int
		succeeded int
		failed    int
		failures  []response.ScanAllFailure
	)

	g, gctx := errgroup.WithContext(ctx)
	sem := semaphore.NewWeighted(int64(workers))
	for _, ref := range targets {
		if err := sem.Acquire(gctx, 1); err != nil {
			break
		}
		g.Go(func() error {
			defer sem.Release(1)
			scanIn := ScanInput{
				Ref:          ref,
				Refresh:      in.Refresh,
				MetadataOnly: in.MetadataOnly,
			}
			_, runErr := runScan(gctx, deps, scanIn)

			mu.Lock()
			done++
			if runErr != nil {
				failed++
				failures = append(failures, classifyScanAllFailure(ref, runErr))
			} else {
				succeeded++
			}
			current := done
			mu.Unlock()
			progress.Update(ctx, "scan_all", ref.String(), current, total)
			return nil
		})
	}
	_ = g.Wait()

	if err := ctx.Err(); err != nil {
		return response.ScanAllResult{}, err
	}

	progress.Success(ctx, "scan_all", "", total)
	return response.ScanAllResult{
		Total:     total,
		Succeeded: succeeded,
		Failed:    failed,
		Failures:  failures,
	}, nil
}

func classifyScanAllFailure(ref refs.Series, err error) response.ScanAllFailure {
	kind := errkind.KindInternal
	if coded, ok := errors.AsType[errkind.Coded](err); ok {
		kind = coded.Kind()
	}
	return response.ScanAllFailure{
		Ref:     ref.String(),
		Kind:    kind,
		Message: err.Error(),
	}
}
