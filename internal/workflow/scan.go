package workflow

import (
	"context"
	"errors"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/jobs"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/scan"
)

// ScanInput parameters for the Scan workflow. Replace=true allows the
// scan to overwrite an existing active record at a different path on
// the same episode slot.
type ScanInput struct {
	Ref     refs.Series
	Replace bool
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
	return jobs.Submit(deps.Jobs, jobs.KindScan, in.Ref, func(jobCtx context.Context) (response.ScanResult, error) {
		source, err := deps.Provider()
		if err != nil {
			return response.ScanResult{}, err
		}
		runner := scan.NewRunner(deps.LibRoot, in.Ref, source, deps.Inspector, deps.Now)
		var out response.ScanResult
		err = deps.Coordinator.WithSeriesRetry(in.Ref, func() error {
			internal, runErr := runner.Scan(jobCtx, scan.Input{
				Replace: in.Replace,
				Mutator: coord.NewMutator("scan"),
			})
			if runErr != nil {
				return translateScanError(runErr)
			}
			out = toScanResponse(internal)
			return nil
		})
		if err != nil {
			return response.ScanResult{}, err
		}
		return out, nil
	})
}

func toScanResponse(in scan.Result) response.ScanResult {
	out := response.ScanResult{
		Synced:      make([]response.ScannedEpisode, 0, len(in.Synced)),
		Skipped:     make([]response.ScanSkip, 0, len(in.Skipped)),
		OrphanSlots: append([]refs.Episode(nil), in.OrphanSlots...),
	}
	for _, ep := range in.Synced {
		out.Synced = append(out.Synced, response.ScannedEpisode{
			Status:     response.ScanStatus(ep.Status),
			Episode:    ep.Episode,
			Source:     ep.Source,
			Resolution: ep.Resolution,
			Path:       ep.Path,
			Companions: ep.Companions,
		})
	}
	for _, skip := range in.Skipped {
		out.Skipped = append(out.Skipped, response.ScanSkip{
			Path:   skip.Path,
			Code:   skip.Code,
			Reason: skip.Reason,
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
