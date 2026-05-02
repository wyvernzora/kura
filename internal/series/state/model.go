package state

import (
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/domain/series"
	"github.com/wyvernzora/kura/internal/metadata"
)

// Transitional aliases. Callers should migrate to series.Series / series.Episode /
// series.SpineEntry directly; these aliases get deleted when state package goes away.
type State = series.Series
type Episode = series.Episode
type SpineEpisode = series.SpineEntry

func NewFromMetadata(ref refs.Metadata, metadataSeries metadata.Series) (State, error) {
	out := State{
		Metadata:       ref,
		PreferredTitle: metadataSeries.PreferredTitle,
		CanonicalTitle: metadataSeries.CanonicalTitle,
		LastScanned:    time.Now().UTC(),
		Episodes:       map[refs.Episode]series.Episode{},
	}
	var spine []series.SpineEntry
	for _, season := range metadataSeries.Seasons {
		for _, episode := range season.Episodes {
			if episode.Ref.IsZero() {
				return State{}, fmt.Errorf("series: metadata has invalid episode ref")
			}
			airDate, err := parseAirDate(episode.Aired)
			if err != nil {
				return State{}, fmt.Errorf("series: invalid air date %q: %w", episode.Aired, err)
			}
			spine = append(spine, series.SpineEntry{Ref: episode.Ref, AirDate: airDate})
		}
	}
	out.RefreshSpine(spine)
	return out, nil
}

// ParseAirDate parses a YYYY-MM-DD or empty string into a civil.Date. Empty
// returns the zero value (which IsValid reports as false).
func ParseAirDate(value string) (civil.Date, error) {
	return parseAirDate(value)
}

func parseAirDate(value string) (civil.Date, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return civil.Date{}, nil
	}
	return civil.ParseDate(value)
}
