package workflow

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/scan"
	"github.com/wyvernzora/kura/internal/storage/paths"
)

// ScanInput parameters for the Scan workflow. Refresh=true forces
// every active record's mediainfo and source detection to re-run
// even when the file's size + mtime are unchanged. Source merging
// preserves a non-Unknown source when the new probe yields Unknown,
// so re-running scan to fix bad data does not lose good data.
type ScanInput struct {
	Ref refs.Series
	// Refresh forces re-probe of every active record (mediainfo + source
	// detection) even when size+mtime are unchanged.
	Refresh bool
	// MetadataOnly skips the filesystem walk + mediainfo probes.
	// Provider spine + artwork + alias data still get refreshed and
	// searchKey recomputed; active records are left untouched.
	MetadataOnly bool
	// Ordering, when non-empty, mutates the persisted per-series spine
	// ordering ("default", "dvd", "absolute", "alternate", "official",
	// "regional") and triggers a fresh provider fetch under the new
	// ordering. Empty preserves whatever the series was last pinned to.
	Ordering string
}

// Scan walks a tracked series's filesystem, refreshes its
// metadata-derived spine from the provider, applies the discovered
// files to the in-memory model, and persists the updated series.json
// via hash CAS. Conflicts (peer mutated mid-walk) trigger one silent
// retry; second conflict surfaces.
//
// Returns a tracked *jobs.Job; callers either Wait for the typed
// result (CLI) or hand the job's ID off to a polling client (long
// MCP tool, future REST). Provider construction and the runner walk
// happen inside the Submit closure so the goroutine, not the caller,
// pays for I/O.
func Scan(ctx context.Context, deps Deps, in ScanInput) *jobs.Job[response.ScanResult] {
	return jobs.Submit(deps.Jobs, ctx, jobs.KindScan, in.Ref, func(jobCtx context.Context) (response.ScanResult, error) {
		return runScan(jobCtx, deps, in)
	})
}

// runScan is the unwrapped scan body. workflow.Scan wraps it with
// jobs.Submit (one tracked job per call); workflow.ScanAll calls it
// inline per ref so a library-wide scan reports one aggregate job
// rather than N tracked jobs.
func runScan(ctx context.Context, deps Deps, in ScanInput) (response.ScanResult, error) {
	seriesRoot := paths.SeriesDir(deps.LibRoot, in.Ref)
	source, err := deps.Provider()
	if err != nil {
		return response.ScanResult{}, err
	}
	runner := scan.NewRunner(deps.LibRoot, in.Ref, source, deps.Inspector, deps.Now, deps.Logger, deps.PreferredLanguages)
	var out response.ScanResult
	var modelForIndex *series.Series
	err = deps.Coordinator.WithSeries(ctx, in.Ref, func() error {
		if err := coord.RetryOnConflict(coord.AttemptsFromEnv(), func() error {
			internal, runErr := runner.Scan(ctx, scan.Input{
				Refresh:      in.Refresh,
				MetadataOnly: in.MetadataOnly,
				Ordering:     in.Ordering,
				Mutator:      coord.NewMutator("scan"),
			})
			if runErr != nil {
				return translateScanError(runErr)
			}
			out = toScanResponse(seriesRoot, internal)
			modelForIndex = internal.Model
			return nil
		}); err != nil {
			return err
		}
		if modelForIndex != nil {
			// ponytail: scan_all persists once per series; batch the index write if bulk scan ever becomes hot.
			if err := updateIndexModel(ctx, deps, modelForIndex, "scan"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return response.ScanResult{}, err
	}
	return out, nil
}

func toScanResponse(seriesRoot string, in scan.Result) response.ScanResult {
	out := response.ScanResult{
		Synced:      make([]response.ScannedEpisode, 0, len(in.Synced)),
		Skipped:     make([]response.ScanSkip, 0, len(in.Skipped)),
		OrphanSlots: append([]refs.Episode(nil), in.OrphanSlots...),
	}
	for _, ep := range in.Synced {
		companions := make([]string, 0, len(ep.Companions))
		for _, c := range ep.Companions {
			companions = append(companions, seriesSelector(seriesRoot, c))
		}
		out.Synced = append(out.Synced, response.ScannedEpisode{
			Status:     response.ScanStatus(ep.Status),
			Episode:    ep.Episode,
			Source:     ep.Source,
			Resolution: ep.Resolution,
			Path:       seriesSelector(seriesRoot, ep.Path),
			Companions: companions,
		})
	}
	for _, skip := range in.Skipped {
		out.Skipped = append(out.Skipped, response.ScanSkip{
			Path:       seriesSelector(seriesRoot, skip.Path),
			Code:       skip.Code,
			Reason:     skip.Reason,
			Source:     skip.Source,
			Resolution: skip.Resolution,
			Size:       skip.Size,
		})
	}
	return out
}

// translateScanError converts scan-package error types into
// workflow-package error types so surfaces can errors.As against the
// canonical workflow types.
func translateScanError(err error) error {
	if alreadyExists, ok := errors.AsType[scan.EpisodeAlreadyExistsError](err); ok {
		return &EpisodeAlreadyExistsError{Episode: alreadyExists.Episode}
	}
	return err
}
