package series

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/series/state"
)

type SpineEpisode = state.SpineEpisode

type editor struct {
	series *seriesState
}

func (e editor) refreshSpine(spine []SpineEpisode) {
	state.Editor{Series: e.series}.RefreshSpine(spine)
}

func (e editor) setStaged(ref refs.Episode, record MediaRecord) error {
	return state.Editor{Series: e.series}.SetStaged(ref, record)
}

func (e editor) clearStaged(ref refs.Episode) error {
	return state.Editor{Series: e.series}.ClearStaged(ref)
}
