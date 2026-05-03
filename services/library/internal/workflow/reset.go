package workflow

import (
	"context"
	"sort"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// ResetInput parameters for the Reset workflow. Episode/All target the
// staged-episode side; TrashIDs/ExtraIDs target the new staging arrays.
// Episode and All are mutually exclusive; All=true drops every staged
// record across all three kinds (episodes + trash + extras).
type ResetInput struct {
	Ref      refs.Series
	Episode  refs.Episode
	All      bool
	TrashIDs []ulid.ULID
	ExtraIDs []ulid.ULID
}

// Reset clears one or every staged record on a series and persists the
// updated series.json. Returns the dropped record(s) so callers can
// surface what was undone.
func Reset(ctx context.Context, deps Deps, in ResetInput) (response.ResetResult, error) {
	seriesRoot := paths.SeriesDir(deps.LibRoot, in.Ref)
	var out response.ResetResult
	err := deps.Coordinator.WithSeries(ctx, in.Ref, func() error {
		return coord.RetryOnConflict(coord.AttemptsFromEnv(), func() error {
			model, err := seriesfile.Load(deps.LibRoot, in.Ref)
			if err != nil {
				return err
			}
			if model.InProgress != nil {
				return &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: *model.InProgress}
			}
			if in.All {
				result, err := resetAllInPlace(ctx, deps, in.Ref, seriesRoot, model)
				if err != nil {
					return err
				}
				out = result
				return nil
			}
			// Targeted ID-based removal of stagedTrash / stagedExtras.
			// May coexist with Episode targeting in the same call.
			var trashRemoved, extraRemoved []string
			for _, id := range in.TrashIDs {
				if model.RemoveStagedTrash(id) {
					trashRemoved = append(trashRemoved, id.String())
				}
			}
			for _, id := range in.ExtraIDs {
				if model.RemoveStagedExtra(id) {
					extraRemoved = append(extraRemoved, id.String())
				}
			}
			var droppedEpisodeView *response.MediaShow
			if !in.Episode.IsZero() {
				episode, ok := model.Episodes[in.Episode]
				if !ok {
					return &MetadataMissingEpisodeError{Episode: in.Episode}
				}
				if episode.Staged == nil {
					return &NoStagedEpisodeError{Episode: in.Episode}
				}
				dropped := media.CloneRecord(*episode.Staged)
				if err := model.ClearStaged(in.Episode); err != nil {
					return err
				}
				view := mediaShow(seriesRoot, dropped)
				droppedEpisodeView = &view
			} else if len(trashRemoved) == 0 && len(extraRemoved) == 0 {
				// Nothing requested. Stay backwards-compatible: callers
				// that didn't pass Episode + didn't pass any IDs hit the
				// "no staged for episode" path with the zero ref.
				return &NoStagedEpisodeError{Episode: in.Episode}
			}
			if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("reset")); err != nil {
				return err
			}
			if err := updateIndexRow(ctx, deps, model, "reset"); err != nil {
				return err
			}
			out = response.ResetResult{
				Record:       droppedEpisodeView,
				TrashRemoved: trashRemoved,
				ExtraRemoved: extraRemoved,
			}
			return nil
		})
	})
	if err != nil {
		return response.ResetResult{}, err
	}
	return out, nil
}

func resetAllInPlace(ctx context.Context, deps Deps, _ refs.Series, seriesRoot string, model *domainseries.Series) (response.ResetResult, error) {
	refsWithStaged := make([]refs.Episode, 0, len(model.Episodes))
	for r, episode := range model.Episodes {
		if episode.Staged != nil {
			refsWithStaged = append(refsWithStaged, r)
		}
	}
	sort.Slice(refsWithStaged, func(i, j int) bool { return refsWithStaged[i].String() < refsWithStaged[j].String() })
	records := make([]response.ResetRecord, 0, len(refsWithStaged))
	for _, r := range refsWithStaged {
		dropped := media.CloneRecord(*model.Episodes[r].Staged)
		if err := model.ClearStaged(r); err != nil {
			return response.ResetResult{}, err
		}
		records = append(records, response.ResetRecord{Episode: r, Record: mediaShow(seriesRoot, dropped)})
	}
	trashRemoved := make([]string, 0, len(model.StagedTrash))
	for _, item := range model.StagedTrash {
		trashRemoved = append(trashRemoved, item.ID.String())
	}
	model.ClearStagedTrash()
	extraRemoved := make([]string, 0, len(model.StagedExtras))
	for _, item := range model.StagedExtras {
		extraRemoved = append(extraRemoved, item.ID.String())
	}
	model.ClearStagedExtras()
	if len(records) > 0 || len(trashRemoved) > 0 || len(extraRemoved) > 0 {
		if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("reset")); err != nil {
			return response.ResetResult{}, err
		}
		if err := updateIndexRow(ctx, deps, model, "reset"); err != nil {
			return response.ResetResult{}, err
		}
	}
	return response.ResetResult{Records: records, TrashRemoved: trashRemoved, ExtraRemoved: extraRemoved}, nil
}
