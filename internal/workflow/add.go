package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/series/layout"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// AddInput parameters for the Add workflow. Ref is optional; when zero,
// the series directory name is derived from the provider's preferred
// title.
type AddInput struct {
	Metadata refs.Metadata
	Ref      refs.Series
}

// Add registers a new series in the library: fetches provider metadata,
// validates the chosen directory name, creates the directory, writes
// series.json, and updates the index. Errors out if the directory
// already exists or the metadata ref is already tracked at a different
// path.
//
// Provider-needing.
func Add(ctx context.Context, deps Deps, in AddInput) (response.AddResult, error) {
	progress.Start(ctx, "add", "Fetching series metadata", 0)
	metadataSeries, metadataRef, err := fetchSeriesMetadata(ctx, deps, in.Metadata)
	if err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return response.AddResult{}, err
	}
	ref := in.Ref
	if ref.IsZero() {
		title, err := layout.ParseFileTitle(metadataSeries.PreferredTitle.String())
		if err != nil {
			progress.Failure(ctx, "add", "Failed to add series", 0, 0)
			return response.AddResult{}, err
		}
		ref, err = refs.ParseSeries(title.String())
		if err != nil {
			progress.Failure(ctx, "add", "Failed to add series", 0, 0)
			return response.AddResult{}, err
		}
	}
	if _, err := refs.ParseSeries(ref.String()); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return response.AddResult{}, err
	}
	target := paths.SeriesDir(deps.LibRoot, ref)
	if _, err := os.Stat(target); err == nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return response.AddResult{}, &SeriesAlreadyExistsError{Ref: ref}
	} else if !errors.Is(err, os.ErrNotExist) {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return response.AddResult{}, err
	}
	if err := checkMetadataAvailable(deps, metadataRef, ref); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return response.AddResult{}, err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 0, 0)
		return response.AddResult{}, err
	}
	progress.Update(ctx, "add", fmt.Sprintf("Writing metadata for %s", ref), 1, 0)
	if err := seriesfile.Initialize(deps.LibRoot, ref, metadataRef, metadataSeries); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 1, 0)
		return response.AddResult{}, err
	}
	if err := deps.Index.Put(metadataRef, ref); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 1, 0)
		return response.AddResult{}, err
	}
	if err := deps.Index.Save(); err != nil {
		progress.Failure(ctx, "add", "Failed to add series", 1, 0)
		return response.AddResult{}, err
	}
	progress.Success(ctx, "add", fmt.Sprintf("Added %s", ref), 1)
	return response.AddResult{
		MetadataRef:    metadataRef,
		Ref:            ref,
		Root:           target,
		PreferredTitle: metadataSeries.PreferredTitle.String(),
	}, nil
}

// fetchSeriesMetadata pulls a full Series view from the provider for
// the given metadata ref. Returns the validated ref unchanged.
func fetchSeriesMetadata(ctx context.Context, deps Deps, ref refs.Metadata) (provider.Series, refs.Metadata, error) {
	if ref.Provider() == "" || ref.ID() == "" {
		return provider.Series{}, "", fmt.Errorf("invalid metadata ref %q; expected <provider>:<id>", ref)
	}
	source, err := deps.Provider()
	if err != nil {
		return provider.Series{}, "", err
	}
	if ref.Provider() != source.Key() {
		return provider.Series{}, "", &UnsupportedMetadataSourceError{Source: ref.Provider()}
	}
	m, err := source.GetSeries(ctx, ref.ID())
	if err != nil {
		return provider.Series{}, "", err
	}
	return m, ref, nil
}

// checkMetadataAvailable rejects an operation that would put metadataRef
// at next when the index already has it pointing at a different series.
func checkMetadataAvailable(deps Deps, metadataRef refs.Metadata, next refs.Series) error {
	existing, ok, err := deps.Index.Get(metadataRef)
	if err != nil {
		return err
	}
	if ok && existing != next {
		return &MetadataRefConflictError{Ref: metadataRef, Existing: existing, Next: next}
	}
	return nil
}
