package layout

import (
	"fmt"

	"github.com/wyvernzora/kura/internal/library/media"
)

type MediaFilename string

type MediaFilenameFacts struct {
	Source     media.MediaSource
	Resolution media.Resolution
}

func BuildMediaFilename(title FilesystemTitle, episode EpisodeRef, facts MediaFilenameFacts, extension string) MediaFilename {
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
