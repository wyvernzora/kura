package workflows

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	media "github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/library/layout"
	"github.com/wyvernzora/kura/internal/library/models"
)

// AddEpisodeOptions describes a media record to add or replace in a series.
type AddEpisodeOptions struct {
	Season     int
	Episode    int
	Path       string
	Source     string
	Companions []string
	MediaInfo  *MediaInfo
	Replace    bool
	Refresh    bool
	Trash      *Trash
}

type EpisodeAlreadyExistsError struct {
	Season  int
	Episode int
}

func (err EpisodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("episode S%02dE%02d already exists; pass --replace to replace it", err.Season, err.Episode)
}

// AddEpisode records an existing media path in a series document and returns the
// updated document. The path is relative to the series root.
func AddEpisode(seriesDir string, series Series, opts AddEpisodeOptions) (Series, error) {
	seriesPath, err := layout.ParseSeriesDir(seriesDir)
	if err != nil {
		return Series{}, err
	}
	if opts.Season < 0 {
		return Series{}, fmt.Errorf("library: invalid season %d", opts.Season)
	}
	if opts.Episode < 1 {
		return Series{}, fmt.Errorf("library: invalid episode %d", opts.Episode)
	}

	relPath, err := layout.CleanSeriesRelPath(opts.Path)
	if err != nil {
		return Series{}, err
	}
	fullPath := filepath.Join(seriesPath.Path(), filepath.FromSlash(relPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		return Series{}, err
	}
	if info.IsDir() {
		return Series{}, fmt.Errorf("library: episode path %q is a directory", relPath)
	}

	if series.Seasons == nil {
		series.Seasons = map[string]Season{}
	}
	season := Season{}
	if opts.Season == 0 {
		if series.Specials != nil {
			season = *series.Specials
		}
	} else {
		season = series.Seasons[strconv.Itoa(opts.Season)]
	}
	if season.Episodes == nil {
		season.Episodes = map[string]Episode{}
	}
	companions, err := companionFiles(seriesPath.Path(), opts.Companions)
	if err != nil {
		return Series{}, err
	}

	mediaFile := MediaFile{
		Path:      relPath,
		Source:    media.ParseMediaSource(opts.Source).String(),
		Size:      info.Size(),
		MTime:     info.ModTime().UTC().Format(time.RFC3339),
		MediaInfo: opts.MediaInfo,
	}
	episodeKey := strconv.Itoa(opts.Episode)
	episode, exists := season.Episodes[episodeKey]
	samePath := exists && media.CleanFilesystemTitle(episode.Media.Path).EqualName(relPath)
	if exists && !opts.Replace && !(opts.Refresh && samePath) {
		return Series{}, EpisodeAlreadyExistsError{Season: opts.Season, Episode: opts.Episode}
	}
	var replaced *Episode
	if exists {
		if opts.Replace {
			if opts.Trash == nil {
				return Series{}, fmt.Errorf("library: trash document is required to replace S%02dE%02d", opts.Season, opts.Episode)
			}
			existing := episode
			replaced = &existing
			episode = Episode{}
		} else if opts.Refresh {
			episode = Episode{}
		} else {
			return Series{}, EpisodeAlreadyExistsError{Season: opts.Season, Episode: opts.Episode}
		}
	}
	episode.Media = mediaFile
	if len(companions) > 0 || episode.Companions == nil || opts.Refresh {
		episode.Companions = companions
	}
	season.Episodes[episodeKey] = episode
	if opts.Season == 0 {
		series.Specials = &season
	} else {
		series.Seasons[strconv.Itoa(opts.Season)] = season
	}

	if err := series.Validate(); err != nil {
		return Series{}, err
	}
	if replaced != nil {
		trash := *opts.Trash
		trash.Entries = append(append([]TrashedEpisode(nil), opts.Trash.Entries...), models.NewTrashedEpisode(opts.Season, opts.Episode, *replaced))
		if err := trash.Validate(); err != nil {
			return Series{}, err
		}
		*opts.Trash = trash
	}
	return series, nil
}

func companionFiles(seriesDir string, paths []string) ([]CompanionFile, error) {
	if len(paths) == 0 {
		return []CompanionFile{}, nil
	}
	out := make([]CompanionFile, 0, len(paths))
	for _, path := range paths {
		relPath, err := layout.CleanSeriesRelPath(path)
		if err != nil {
			return nil, err
		}
		fullPath := filepath.Join(seriesDir, filepath.FromSlash(relPath))
		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("library: companion path %q is a directory", relPath)
		}
		out = append(out, CompanionFile{
			Path:  relPath,
			Size:  info.Size(),
			MTime: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}
