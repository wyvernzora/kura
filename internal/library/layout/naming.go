package layout

import (
	"fmt"
	"strings"

	"github.com/wyvernzora/kura/internal/library/media"
)

type MediaFilename string

type MediaFilenameFacts struct {
	Source     media.MediaSource
	VideoCodec media.Codec
	Resolution media.Resolution
}

func BuildMediaFilename(title FilesystemTitle, episode EpisodeRef, facts MediaFilenameFacts, extension string) MediaFilename {
	source := facts.Source.Display()
	videoCodec := strings.TrimSpace(facts.VideoCodec.String())
	if videoCodec == "" {
		videoCodec = "UnknownCodec"
	}
	resolution := facts.Resolution.String()
	if resolution == "" {
		resolution = "UnknownResolution"
	}
	return MediaFilename(fmt.Sprintf(
		"%s - %s (%s %s %s)%s",
		title,
		episode.Marker(),
		source,
		videoCodec,
		resolution,
		extension,
	))
}

func (f MediaFilename) String() string {
	return string(f)
}
