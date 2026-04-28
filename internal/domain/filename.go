package domain

import (
	"fmt"
)

type MediaFilename string

type MediaFilenameFacts struct {
	Source     MediaSource
	Resolution Resolution
}

func BuildMediaFilename(title FileTitle, episode EpisodeRef, facts MediaFilenameFacts, extension string) MediaFilename {
	source := facts.Source.Display()
	resolution := facts.Resolution.Display()
	if resolution == "" {
		resolution = "UnknownResolution"
	}
	return MediaFilename(fmt.Sprintf(
		"%s - %s (%s %s)%s",
		title,
		episode.Marker(),
		source,
		resolution,
		extension,
	))
}

func (f MediaFilename) String() string {
	return string(f)
}
