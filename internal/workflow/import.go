package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// ImportInput parameters for the Import workflow. Ref is required —
// import operates on an existing directory the user named explicitly.
// Force=true replaces an existing .kura/series.json (preserving other
// .kura contents like trash).
type ImportInput struct {
	Metadata refs.Metadata
	Ref      refs.Series
	Force    bool
}

// Import takes an existing directory under the library root and starts
// tracking it. Errors out unless the directory exists and (without
// Force) has no .kura/series.json.
//
// Provider-needing.
func Import(ctx context.Context, deps Deps, in ImportInput) (response.AddResult, error) {
	if in.Ref.IsZero() {
		return response.AddResult{}, errors.New("workflow: series ref is required")
	}
	ref, err := refs.ParseSeries(in.Ref.String())
	if err != nil {
		return response.AddResult{}, err
	}
	progress.Start(ctx, "import", fmt.Sprintf("Fetching metadata for %s", ref), 0)
	metadataSeries, metadataRef, err := fetchSeriesMetadata(ctx, deps, in.Metadata)
	if err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		return response.AddResult{}, err
	}
	if _, err := layout.ParseSeriesDir(paths.SeriesDir(deps.LibRoot, ref)); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		if errors.Is(err, os.ErrNotExist) {
			return response.AddResult{}, &SeriesNotFoundError{Ref: ref}
		}
		return response.AddResult{}, err
	}
	metadataPath := paths.SeriesMetadata(deps.LibRoot, ref)
	if _, err := os.Stat(metadataPath); err == nil {
		if !in.Force {
			progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
			return response.AddResult{}, &SeriesAlreadyTrackedError{Ref: ref}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		return response.AddResult{}, err
	}
	if err := checkMetadataAvailable(deps, metadataRef, ref); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
		return response.AddResult{}, err
	}
	if in.Force {
		deps.Index.Remove(ref)
		if err := os.Remove(metadataPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 0, 0)
			return response.AddResult{}, err
		}
	}
	progress.Update(ctx, "import", fmt.Sprintf("Writing metadata for %s", ref), 1, 0)
	if err := seriesfile.Initialize(deps.LibRoot, ref, metadataRef, metadataSeries); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 1, 0)
		return response.AddResult{}, err
	}
	if err := deps.Index.Put(metadataRef, ref); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 1, 0)
		return response.AddResult{}, err
	}
	if err := deps.Index.Save(); err != nil {
		progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), 1, 0)
		return response.AddResult{}, err
	}
	progress.Success(ctx, "import", fmt.Sprintf("Imported %s", ref), 1)
	return response.AddResult{
		MetadataRef:    metadataRef,
		Ref:            ref,
		Root:           paths.SeriesDir(deps.LibRoot, ref),
		PreferredTitle: metadataSeries.PreferredTitle.String(),
	}, nil
}
