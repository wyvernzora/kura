package series

import (
	"github.com/wyvernzora/kura/internal/series/state"
	"github.com/wyvernzora/kura/internal/series/wire"
)

func fromWire(in wire.SeriesV1) (seriesState, error) {
	return state.FromWire(in)
}

func toWire(in seriesState) (wire.SeriesV1, error) {
	return state.ToWire(in)
}
