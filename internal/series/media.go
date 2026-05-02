package series

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/series/layout"
)

type FileTitle = layout.FileTitle

type MediaFilename = layout.MediaFilename

type MediaFilenameFacts = layout.MediaFilenameFacts

func ParseFileTitle(title string) (FileTitle, error) {
	return layout.ParseFileTitle(title)
}

func CleanFileTitle(title string) FileTitle {
	return layout.CleanFileTitle(title)
}

func BuildMediaFilename(title FileTitle, episode refs.Episode, facts MediaFilenameFacts, extension string) MediaFilename {
	return layout.BuildMediaFilename(title, episode, facts, extension)
}
