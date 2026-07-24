// Package refs is the public wire/vocabulary facade over internal/domain/refs;
// the internal package remains the implementation.
package refs

import irefs "github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"

type (
	Series   = irefs.Series
	Metadata = irefs.Metadata
	Episode  = irefs.Episode
)

func ParseSeries(value string) (Series, error) {
	return irefs.ParseSeries(value)
}

func ParseMetadata(value string) (Metadata, error) {
	return irefs.ParseMetadata(value)
}

func ParseEpisodeMarker(value string) (Episode, error) {
	return irefs.ParseEpisodeMarker(value)
}
