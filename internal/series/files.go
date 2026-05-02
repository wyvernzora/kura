package series

import (
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/series/layout"
)

type files struct {
	root string
}

type fileFacts = layout.FileFacts

func (f files) seriesDir(ref refs.Series) (SeriesDir, error) {
	return layout.NewFiles(f.root).SeriesDir(ref)
}

func (f files) stat(path string) (fileFacts, error) {
	return layout.NewFiles(f.root).Stat(path)
}
