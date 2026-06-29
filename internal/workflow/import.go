package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
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

// replaceImportRow returns the index-CAS callback Import uses on the
// final commit: drops any prior row pointing at `ref` (Force-replace
// path), rejects conflicting rows for `metadataRef` pointing elsewhere,
// and appends the new row.
func replaceImportRow(ref refs.Series, metadataRef refs.Metadata, indexRow indexfile.Row) func(indexfile.Loaded) ([]indexfile.Row, error) {
	return func(loaded indexfile.Loaded) ([]indexfile.Row, error) {
		filtered := make([]indexfile.Row, 0, len(loaded.Rows))
		for _, row := range loaded.Rows {
			if row.Series == ref {
				continue
			}
			if row.Metadata == metadataRef && row.Series != ref {
				return nil, &MetadataRefConflictError{Ref: metadataRef, Existing: row.Series, Next: ref}
			}
			filtered = append(filtered, row)
		}
		return append(filtered, indexRow), nil
	}
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
	if in.Force {
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
	model.RecomputeSearchKey(deps.PreferredLanguages, metadataSeries.Aliases, metadataSeries.TranslatedTitles)
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("import")); err != nil {
		return response.AddResult{}, err
	}
	indexRow := indexfile.BuildRowFromModelWithOptions(model, deps.Now(), rowBuildOptions(deps))
	if err := withIndexCAS(ctx, deps, "import", replaceImportRow(ref, metadataRef, indexRow)); err != nil {
		return response.AddResult{}, err
	}
	progress.Success(ctx, "import", fmt.Sprintf("Imported %s", ref), 1)
	return response.AddResult{
		MetadataRef:    metadataRef,
		Ref:            ref,
		PreferredTitle: metadataSeries.PreferredTitle.String(),
	}, nil
}
