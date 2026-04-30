package series

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/refs"
)

type files struct {
	root fsroot.LibraryRoot
}

type fileFacts struct {
	Size  int64
	MTime time.Time
}

func (f files) seriesDir(ref refs.Series) (fsroot.SeriesDir, error) {
	return f.root.SeriesDir(ref.String())
}

func (f files) stat(path string) (fileFacts, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFacts{}, err
	}
	if info.IsDir() {
		return fileFacts{}, fmt.Errorf("series: path %q is a directory", path)
	}
	return fileFacts{Size: info.Size(), MTime: info.ModTime().UTC()}, nil
}

func (f files) canonicalPath(ref refs.Series, episode refs.Episode, media MediaRecord) (string, error) {
	title := domain.CleanFileTitle(ref.String())
	season, err := domain.NewSeasonNumber(episode.Season())
	if err != nil {
		return "", err
	}
	number, err := domain.NewEpisodeNumber(episode.Episode())
	if err != nil {
		return "", err
	}
	facts := domain.MediaFilenameFacts{Source: domain.ParseMediaSource(media.Source)}
	if media.Resolution != "" {
		if resolution, err := domain.ParseResolution(media.Resolution); err == nil {
			facts.Resolution = resolution
		}
	}
	filename := domain.BuildMediaFilename(title, domain.NewEpisodeRef(season, number), facts, filepath.Ext(media.Path)).String()
	if episode.Season() == 0 {
		return filename, nil
	}
	return filepath.ToSlash(filepath.Join(fmt.Sprintf("Season %d", episode.Season()), filename)), nil
}

func (f files) move(from, to string) error {
	return fsroot.SafeMoveFile(from, to)
}
