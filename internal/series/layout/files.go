package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wyvernzora/kura/internal/domain/filename"
	"github.com/wyvernzora/kura/internal/domain/media"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/storage/paths"
	"github.com/wyvernzora/kura/internal/storage/seriesdir"
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

func (f Files) SeriesDir(ref refs.Series) (seriesdir.SeriesDir, error) {
	return seriesdir.Parse(paths.SeriesDir(f.root, ref))
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

func (f Files) CanonicalPath(ref refs.Series, episode refs.Episode, record media.Record) (string, error) {
	title := filename.CleanTitle(ref.String())
	basename := filename.BuildMedia(title, episode, filename.Facts{
		Source:     record.Source,
		Resolution: record.Resolution,
	}, filepath.Ext(record.Path)).String()
	return paths.EpisodeMediaRel(episode.Season(), basename), nil
}
