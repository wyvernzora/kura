package series

import (
	"context"
	"fmt"
	"sort"

	"github.com/wyvernzora/kura/internal/domain/refs"
)

type ResetInput struct {
	Episode refs.Episode
	All     bool
}

type ResetResult struct {
	Series  refs.Series   `json:"series"`
	Applied bool          `json:"applied"`
	Episode *refs.Episode `json:"episode,omitempty"`
	Record  *MediaRecord  `json:"record,omitempty"`
	Records []ResetRecord `json:"records,omitempty"`
}

type ResetRecord struct {
	Episode refs.Episode `json:"episode"`
	Record  MediaRecord  `json:"record"`
}

type NoStagedEpisodeError struct {
	Episode refs.Episode
}

func (err NoStagedEpisodeError) Error() string {
	return fmt.Sprintf("episode %s has no staged media", err.Episode.Marker())
}

func (h Handle) Reset(ctx context.Context, in ResetInput) (ResetResult, error) {
	_ = ctx
	model, err := h.load()
	if err != nil {
		return ResetResult{}, err
	}
	if in.All {
		return h.resetAll(model)
	}
	episode, ok := model.Episodes[in.Episode]
	if !ok {
		return ResetResult{}, MetadataMissingEpisodeError{Episode: in.Episode}
	}
	if episode.Staged == nil {
		return ResetResult{}, NoStagedEpisodeError{Episode: in.Episode}
	}
	record := cloneMediaRecord(*episode.Staged)
	editor := editor{series: &model}
	if err := editor.clearStaged(in.Episode); err != nil {
		return ResetResult{}, err
	}
	if err := h.repo().save(h.ref, model); err != nil {
		return ResetResult{}, err
	}
	return ResetResult{
		Series:  h.ref,
		Applied: true,
		Episode: &in.Episode,
		Record:  &record,
	}, nil
}

func (h Handle) resetAll(model seriesState) (ResetResult, error) {
	refsWithStaged := make([]refs.Episode, 0, len(model.Episodes))
	for ref, episode := range model.Episodes {
		if episode.Staged != nil {
			refsWithStaged = append(refsWithStaged, ref)
		}
	}
	sort.Slice(refsWithStaged, func(i, j int) bool {
		return refsWithStaged[i].String() < refsWithStaged[j].String()
	})
	records := make([]ResetRecord, 0, len(refsWithStaged))
	editor := editor{series: &model}
	for _, ref := range refsWithStaged {
		record := cloneMediaRecord(*model.Episodes[ref].Staged)
		if err := editor.clearStaged(ref); err != nil {
			return ResetResult{}, err
		}
		records = append(records, ResetRecord{Episode: ref, Record: record})
	}
	if len(records) > 0 {
		if err := h.repo().save(h.ref, model); err != nil {
			return ResetResult{}, err
		}
	}
	return ResetResult{
		Series:  h.ref,
		Applied: len(records) > 0,
		Records: records,
	}, nil
}
