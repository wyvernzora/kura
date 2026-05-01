package series

import (
	"context"
	"fmt"

	"github.com/wyvernzora/kura/internal/refs"
)

type ResetInput struct {
	Episode refs.Episode
}

type ResetResult struct {
	Series  refs.Series  `json:"series"`
	Applied bool         `json:"applied"`
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
		Episode: in.Episode,
		Record:  record,
	}, nil
}
