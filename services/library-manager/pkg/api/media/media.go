// Package media is the public wire/vocabulary facade over internal/domain/media;
// the internal package remains the implementation.
package media

import imedia "github.com/wyvernzora/kura/services/library-manager/internal/domain/media"

type (
	Source     = imedia.Source
	Resolution = imedia.Resolution
)

func ParseSource(value string) Source {
	return imedia.ParseSource(value)
}

func ParseResolution(value string) (Resolution, error) {
	return imedia.ParseResolution(value)
}
