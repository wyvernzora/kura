package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/filename"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/provider"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// AddInput parameters for the Add workflow. Ref is optional; when zero,
// the series directory name is derived from the provider's preferred
// title. Ordering pins the TVDB episode-spine ordering at registration
// time; empty means unset (provider default applied implicitly).
type AddInput struct {
	Metadata refs.Metadata
	Ref      refs.Series
	Ordering string
}

// Add registers a new series in the library: fetches provider metadata,
// validates the chosen directory name, creates the directory, writes
// series.json, and updates the index. Errors out if the directory
// already exists or the metadata ref is already tracked at a different
// path.
//
// Provider-needing.
func Add(ctx context.Context, deps Deps, in AddInput) (result response.AddResult, err error) {
	progress.Start(ctx, "add", "Fetching series metadata", 0)
	// step tracks how far the workflow advanced before failing so the
	// deferred Failure reports the right counter (0 = pre-write, 1 =
	// post-Update). Mirrors the explicit progress.Failure calls the
	// pre-defer version emitted at each step boundary.
	step := 0
	defer func() {
		if err != nil {
			progress.Failure(ctx, "add", "Failed to add series", step, 0)
		}
	}()

	metadataSeries, metadataRef, err := fetchSeriesMetadata(ctx, deps, in.Metadata, in.Ordering)
	if err != nil {
		return response.AddResult{}, err
	}
	ref, err := resolveAddRef(in.Ref, metadataSeries)
	if err != nil {
		return response.AddResult{}, err
	}
	target := paths.SeriesDir(deps.LibRoot, ref)
	if _, statErr := os.Stat(target); statErr == nil {
		return response.AddResult{}, &SeriesAlreadyExistsError{Ref: ref}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return response.AddResult{}, statErr
	}
	if err := checkMetadataAvailable(deps, metadataRef, ref); err != nil {
		return response.AddResult{}, err
	}
	if err := os.MkdirAll(target, 0o775); err != nil {
		return response.AddResult{}, err
	}
	progress.Update(ctx, "add", fmt.Sprintf("Writing metadata for %s", ref), 1, 0)
	step = 1
	model, err := seriesfile.NewFromMetadata(metadataRef, in.Ordering, metadataSeries)
	if err != nil {
		return response.AddResult{}, err
	}
	model.Ref = ref
	// Aliases + translated titles fold into searchKey here and stay
	// transient — neither lands on disk. Next scan refreshes both
	// from TVDB.
	model.RecomputeSearchKey(deps.PreferredLanguages, metadataSeries.Aliases, metadataSeries.TranslatedTitles)
	if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("add")); err != nil {
		return response.AddResult{}, err
	}
	if err := updateIndexModel(ctx, deps, model, "add"); err != nil {
		return response.AddResult{}, translateIndexDuplicate(err)
	}
	progress.Success(ctx, "add", fmt.Sprintf("Added %s", ref), 1)
	return response.AddResult{
		MetadataRef:    metadataRef,
		Ref:            ref,
		PreferredTitle: metadataSeries.PreferredTitle.String(),
	}, nil
}

func translateIndexDuplicate(err error) error {
	var dup indexfile.DuplicateRefError
	if errors.As(err, &dup) {
		return &MetadataRefConflictError{Ref: dup.Ref, Existing: dup.Existing, Next: dup.Next}
	}
	return err
}

// resolveAddRef returns the explicit caller-supplied ref when set,
// otherwise derives it from the provider's preferred title via the
// filename parser. Validates the resulting ref string roundtrips
// through refs.ParseSeries either way.
func resolveAddRef(explicit refs.Series, metadataSeries provider.Series) (refs.Series, error) {
	if !explicit.IsZero() {
		if _, err := refs.ParseSeries(explicit.String()); err != nil {
			return refs.Series{}, err
		}
		return explicit, nil
	}
	title, err := filename.ParseTitle(metadataSeries.PreferredTitle.String())
	if err != nil {
		return refs.Series{}, err
	}
	return refs.ParseSeries(title.String())
}

// fetchSeriesMetadata pulls a full Series view from the provider for
// the given metadata ref under the requested ordering ("" = provider
// default). Returns the validated ref unchanged.
func fetchSeriesMetadata(ctx context.Context, deps Deps, ref refs.Metadata, ordering string) (provider.Series, refs.Metadata, error) {
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
	m, err := source.GetSeries(ctx, ref.ID(), ordering)
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
