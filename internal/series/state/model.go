package state

import (
	"fmt"
	"time"

	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/metadata"
	"github.com/wyvernzora/kura/internal/textnorm"
)

type State struct {
	Metadata       refs.Metadata
	PreferredTitle textnorm.NFCString
	CanonicalTitle textnorm.NFCString
	LastScanned    time.Time
	Episodes       map[refs.Episode]Episode
}

type Episode struct {
	AirDate string
	Active  *media.Record
	Staged  *media.Record
}

func NewFromMetadata(ref refs.Metadata, metadataSeries metadata.Series) (State, error) {
	out := State{
		Metadata:       ref,
		PreferredTitle: metadataSeries.PreferredTitle,
		CanonicalTitle: metadataSeries.CanonicalTitle,
		LastScanned:    time.Now().UTC(),
		Episodes:       map[refs.Episode]Episode{},
	}
	var spine []SpineEpisode
	for _, season := range metadataSeries.Seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return State{}, fmt.Errorf("series: metadata has invalid episode ref")
			}
			spine = append(spine, SpineEpisode{Ref: episode.Ref, AirDate: episode.Aired})
		}
	}
	Editor{Series: &out}.RefreshSpine(spine)
	return out, nil
}
