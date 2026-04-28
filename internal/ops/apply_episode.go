package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/store"
)

// AddEpisodeOptions describes a media record to add or replace in a series.
type AddEpisodeOptions struct {
	Season     int
	Episode    int
	Path       string
	Source     string
	Companions []string
	MediaInfo  *domain.MediaInfo
	Replace    bool
	Refresh    bool
	Trash      *store.Trash
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
func AddEpisode(seriesDir string, series store.Series, opts AddEpisodeOptions) (store.Series, error) {
	seriesPath, err := fsroot.ParseSeriesDir(seriesDir)
	if err != nil {
		return store.Series{}, err
	}
	if opts.Season < 0 {
		return store.Series{}, fmt.Errorf("library: invalid season %d", opts.Season)
	}
	if opts.Episode < 1 {
		return store.Series{}, fmt.Errorf("library: invalid episode %d", opts.Episode)
	}

	relPath, err := fsroot.CleanSeriesRelPath(opts.Path)
	if err != nil {
		return store.Series{}, err
	}
	fullPath := filepath.Join(seriesPath.Path(), filepath.FromSlash(relPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		return store.Series{}, err
	}
	if info.IsDir() {
		return store.Series{}, fmt.Errorf("library: episode path %q is a directory", relPath)
	}

	season := store.Season{}
	if existingSeason, ok := series.Season(opts.Season); ok {
		season = *existingSeason
	}
	season.Number = opts.Season
	companions, err := companionFiles(seriesPath.Path(), opts.Companions)
	if err != nil {
		return store.Series{}, err
	}

	mediaFile := store.MediaFile{
		Path:      relPath,
		Source:    domain.ParseMediaSource(opts.Source).String(),
		Size:      info.Size(),
		MTime:     info.ModTime().UTC().Format(time.RFC3339),
		MediaInfo: opts.MediaInfo,
	}
	episodePtr, exists := season.Episode(opts.Episode)
	episode := store.Episode{}
	if exists {
		episode = *episodePtr
	}
	samePath := exists && domain.CleanFileTitle(episode.Media.Path).EqualName(relPath)
	if exists && !opts.Replace && !(opts.Refresh && samePath) {
		return store.Series{}, EpisodeAlreadyExistsError{Season: opts.Season, Episode: opts.Episode}
	}
	var replaced *store.Episode
	if exists {
		if opts.Replace {
			if opts.Trash == nil {
				return store.Series{}, fmt.Errorf("library: trash document is required to replace S%02dE%02d", opts.Season, opts.Episode)
			}
			existing := episode
			replaced = &existing
			episode = store.Episode{}
		} else if opts.Refresh {
			episode = store.Episode{}
		} else {
			return store.Series{}, EpisodeAlreadyExistsError{Season: opts.Season, Episode: opts.Episode}
		}
	}
	episode.Media = mediaFile
	if len(companions) > 0 || episode.Companions == nil || opts.Refresh {
		episode.Companions = companions
	}
	episode.Number = opts.Episode
	season.UpsertEpisode(episode)
	series.UpsertSeason(season)

	if err := series.Validate(); err != nil {
		return store.Series{}, err
	}
	if replaced != nil {
		trash := *opts.Trash
		trash.Entries = append(append([]store.TrashedEpisode(nil), opts.Trash.Entries...), store.NewTrashedEpisode(opts.Season, opts.Episode, *replaced))
		if err := trash.Validate(); err != nil {
			return store.Series{}, err
		}
		*opts.Trash = trash
	}
	return series, nil
}

func companionFiles(seriesDir string, paths []string) ([]store.CompanionFile, error) {
	if len(paths) == 0 {
		return []store.CompanionFile{}, nil
	}
	out := make([]store.CompanionFile, 0, len(paths))
	for _, path := range paths {
		relPath, err := fsroot.CleanSeriesRelPath(path)
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
		out = append(out, store.CompanionFile{
			Path:  relPath,
			Size:  info.Size(),
			MTime: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}
