package workflow

import (
	"context"
	"sort"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	domainseries "github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesfile"
)

// ResetInput parameters for the Reset workflow. Episode and All are
// mutually exclusive; All=true drops every staged record on the series.
type ResetInput struct {
	Ref     refs.Series
	Episode refs.Episode
	All     bool
}

// Reset clears one or every staged record on a series and persists the
// updated series.json. Returns the dropped record(s) so callers can
// surface what was undone.
func Reset(ctx context.Context, deps Deps, in ResetInput) (response.ResetResult, error) {
	_ = ctx
	seriesRoot := paths.SeriesDir(deps.LibRoot, in.Ref)
	var out response.ResetResult
	err := deps.Coordinator.WithSeriesRetry(in.Ref, func() error {
		model, err := seriesfile.Load(deps.LibRoot, in.Ref)
		if err != nil {
			return err
		}
		if model.InProgress != nil {
			return &coord.BusyError{Scope: coord.SeriesScope(in.Ref), Holder: *model.InProgress}
		}
		if in.All {
			result, err := resetAllInPlace(deps, in.Ref, seriesRoot, model)
			if err != nil {
				return err
			}
			out = result
			return nil
		}
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
		if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("reset")); err != nil {
			return err
		}
		if err := updateIndexRow(deps, model, "reset"); err != nil {
			return err
		}
		view := mediaShow(seriesRoot, dropped)
		out = response.ResetResult{Record: &view}
		return nil
	})
	if err != nil {
		return response.ResetResult{}, err
	}
	return out, nil
}

func resetAllInPlace(deps Deps, _ refs.Series, seriesRoot string, model *domainseries.Series) (response.ResetResult, error) {
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
	if len(records) > 0 {
		if err := seriesfile.SaveCAS(deps.LibRoot, model, coord.NewMutator("reset")); err != nil {
			return response.ResetResult{}, err
		}
		if err := updateIndexRow(deps, model, "reset"); err != nil {
			return response.ResetResult{}, err
		}
	}
	return response.ResetResult{Records: records}, nil
}
