package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
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
	Ordering string
}

// validateImportTarget checks the on-disk preconditions for Import:
// the series directory exists and (without Force) has no series.json.
// Returns the absolute series.json path on success; SeriesNotFoundError
// or SeriesAlreadyTrackedError otherwise.
func validateImportTarget(libRoot string, ref refs.Series, force bool) (string, error) {
	if _, err := seriesdir.Parse(paths.SeriesDir(libRoot, ref)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", &SeriesNotFoundError{Ref: ref}
		}
		return "", err
	}
	metadataPath := paths.SeriesMetadata(libRoot, ref)
	_, statErr := os.Stat(metadataPath)
	switch {
	case statErr == nil:
		if !force {
			return "", &SeriesAlreadyTrackedError{Ref: ref}
		}
	case !errors.Is(statErr, os.ErrNotExist):
		return "", statErr
	}
	return metadataPath, nil
}

// Import takes an existing directory under the library root and starts
// tracking it. Errors out unless the directory exists and (without
// Force) has no .kura/series.json.
//
// Provider-needing.
func Import(ctx context.Context, deps Deps, in ImportInput) (result response.AddResult, err error) {
	if in.Ref.IsZero() {
		return response.AddResult{}, errors.New("workflow: series ref is required")
	}
	ref, err := refs.ParseSeries(in.Ref.String())
	if err != nil {
		return response.AddResult{}, err
	}
	progress.Start(ctx, "import", fmt.Sprintf("Fetching metadata for %s", ref), 0)
	// step tracks how far the workflow advanced so the deferred
	// Failure reports the right counter (0 = pre-write, 1 = post-Update).
	step := 0
	defer func() {
		if err != nil {
			progress.Failure(ctx, "import", fmt.Sprintf("Failed to import %s", ref), step, 0)
		}
	}()

	metadataSeries, metadataRef, err := fetchSeriesMetadata(ctx, deps, in.Metadata, in.Ordering)
	if err != nil {
		return response.AddResult{}, err
	}
	metadataPath, err := validateImportTarget(deps.LibRoot, ref, in.Force)
	if err != nil {
		return response.AddResult{}, err
	}
	if err := checkMetadataAvailable(deps, metadataRef, ref); err != nil {
		return response.AddResult{}, err
	}
	var preservedTags []string
	if in.Force {
		if existing, loadErr := seriesfile.Load(deps.LibRoot, ref); loadErr == nil {
			preservedTags = slices.Clone(existing.Tags)
		}
		if rmErr := os.Remove(metadataPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return response.AddResult{}, rmErr
		}
	}
	progress.Update(ctx, "import", fmt.Sprintf("Writing metadata for %s", ref), 1, 0)
	step = 1
	model, err := seriesfile.NewFromMetadata(metadataRef, in.Ordering, metadataSeries)
	if err != nil {
		return response.AddResult{}, err
	}
	model.Ref = ref
	if in.Force {
		model.Tags = preservedTags
	}
	model.RecomputeSearchKey(deps.PreferredLanguages, metadataSeries.Aliases, metadataSeries.TranslatedTitles)
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("import")); err != nil {
		return response.AddResult{}, err
	}
	if err := updateIndexModel(ctx, deps, model, "import"); err != nil {
		return response.AddResult{}, translateIndexDuplicate(err)
	}
	progress.Success(ctx, "import", fmt.Sprintf("Imported %s", ref), 1)
	return response.AddResult{
		MetadataRef:    metadataRef,
		Ref:            ref,
		PreferredTitle: metadataSeries.PreferredTitle.String(),
	}, nil
}
