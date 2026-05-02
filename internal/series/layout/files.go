package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/media"
	"github.com/wyvernzora/kura/internal/series/state"
)

type Files struct {
	root string
}

func NewFiles(root string) Files {
	return Files{root: root}
}

type FileFacts struct {
	Size  int64
	MTime time.Time
}

func (f Files) SeriesDir(ref refs.Series) (SeriesDir, error) {
	return ParseSeriesDir(filepath.Join(f.root, filepath.FromSlash(ref.String())))
}

func (f Files) Stat(path string) (FileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileFacts{}, err
	}
	if info.IsDir() {
		return FileFacts{}, fmt.Errorf("series: path %q is a directory", path)
	}
	return FileFacts{Size: info.Size(), MTime: info.ModTime().UTC().Truncate(time.Second)}, nil
}

func (f Files) CanonicalPath(ref refs.Series, episode refs.Episode, record state.MediaRecord) (string, error) {
	title := CleanFileTitle(ref.String())
	facts := MediaFilenameFacts{Source: media.ParseSource(record.Source)}
	if record.Resolution != "" {
		if resolution, err := media.ParseResolution(record.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	filename := BuildMediaFilename(title, episode, facts, filepath.Ext(record.Path)).String()
	if episode.Season() == 0 {
		return filename, nil
	}
	return filepath.ToSlash(filepath.Join(fmt.Sprintf("Season %d", episode.Season()), filename)), nil
}

func (f Files) Move(from, to string) error {
	return SafeMoveFile(from, to)
}
