package series

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wyvernzora/kura/internal/refs"
)

type files struct {
	root string
}

type fileFacts struct {
	Size  int64
	MTime time.Time
}

func (f files) seriesDir(ref refs.Series) (SeriesDir, error) {
	return ParseSeriesDir(filepath.Join(f.root, filepath.FromSlash(ref.String())))
}

func (f files) stat(path string) (fileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFacts{}, err
	}
	if info.IsDir() {
		return fileFacts{}, fmt.Errorf("series: path %q is a directory", path)
	}
	return fileFacts{Size: info.Size(), MTime: info.ModTime().UTC().Truncate(time.Second)}, nil
}

func (f files) canonicalPath(ref refs.Series, episode refs.Episode, media MediaRecord) (string, error) {
	title := CleanFileTitle(ref.String())
	facts := MediaFilenameFacts{Source: ParseMediaSource(media.Source)}
	if media.Resolution != "" {
		if resolution, err := ParseResolution(media.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	filename := BuildMediaFilename(title, episode, facts, filepath.Ext(media.Path)).String()
	if episode.Season() == 0 {
		return filename, nil
	}
	return filepath.ToSlash(filepath.Join(fmt.Sprintf("Season %d", episode.Season()), filename)), nil
}

func (f files) move(from, to string) error {
	return safeMoveFile(from, to)
}
