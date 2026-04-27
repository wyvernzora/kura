// Package series owns Kura's persistent per-series library state.
package series

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wyvernzora/kura/internal/library/layout"
	"github.com/wyvernzora/kura/internal/library/media"
)

const (
	SeriesSchemaVersion = 1
)

// Series is the persistent .kura/series.json document for one local series.
//
// It stores local library facts only. Live provider metadata, such as episode
// titles and air dates, belongs in provider read views and is intentionally not
// persisted here.
type Series struct {
	SchemaVersion     int               `json:"schemaVersion"`
	ID                string            `json:"id"`
	ProviderRefs      []string          `json:"providerRefs"`
	PreferredProvider string            `json:"preferredProvider"`
	PreferredTitle    string            `json:"preferredTitle"`
	CanonicalTitle    string            `json:"canonicalTitle"`
	LastScanned       string            `json:"lastScanned,omitempty"`
	Notes             string            `json:"notes,omitempty"`
	Seasons           map[string]Season `json:"seasons,omitempty"`
	Specials          *Season           `json:"specials,omitempty"`

	dirname string
}

func (s Series) MarshalJSON() ([]byte, error) {
	return json.Marshal(seriesToV1(s))
}

// Season stores local state for one regular season or the specials collection.
type Season struct {
	Notes    string             `json:"notes,omitempty"`
	Episodes map[string]Episode `json:"episodes,omitempty"`
}

// Episode stores local state for one episode.
type Episode struct {
	Media      MediaFile       `json:"media"`
	Companions []CompanionFile `json:"companions"`
}

func (e Episode) MarshalJSON() ([]byte, error) {
	type episode Episode
	out := episode(e)
	if out.Companions == nil {
		out.Companions = []CompanionFile{}
	}
	return json.Marshal(out)
}

// MediaFile stores facts about one primary media file.
type MediaFile struct {
	Path       string     `json:"path"`
	Source     string     `json:"source"`
	Size       int64      `json:"size"`
	MTime      string     `json:"mtime"`
	SampleHash string     `json:"sampleHash,omitempty"`
	MediaInfo  *MediaInfo `json:"mediainfo,omitempty"`
}

// CompanionFile stores facts about an associated sidecar file.
type CompanionFile struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

type MediaInfo = media.MediaInfo

// AddEpisodeOptions describes a media record to add or replace in a series.
type AddEpisodeOptions struct {
	Season     int
	Episode    int
	Path       string
	Source     string
	Companions []string
	MediaInfo  *MediaInfo
}

// SeriesPath returns the metadata path for a series directory.
func SeriesPath(seriesDir string) string {
	return layout.SeriesMetadataPath(seriesDir)
}

// AddEpisode records an existing media path in a series document and returns the
// updated document. The path is relative to the series root.
func AddEpisode(seriesDir string, series Series, opts AddEpisodeOptions) (Series, error) {
	seriesDir, err := cleanDirname(seriesDir)
	if err != nil {
		return Series{}, err
	}
	if series.dirname == "" {
		series.dirname = seriesDir
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
	fullPath := filepath.Join(seriesDir, filepath.FromSlash(relPath))
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
	companions, err := companionFiles(seriesDir, opts.Companions)
	if err != nil {
		return Series{}, err
	}

	media := MediaFile{
		Path:      relPath,
		Source:    cleanSource(opts.Source),
		Size:      info.Size(),
		MTime:     info.ModTime().UTC().Format(time.RFC3339),
		MediaInfo: opts.MediaInfo,
	}
	episode := season.Episodes[strconv.Itoa(opts.Episode)]
	episode.Media = media
	if len(companions) > 0 || episode.Companions == nil {
		episode.Companions = companions
	}
	season.Episodes[strconv.Itoa(opts.Episode)] = episode
	if opts.Season == 0 {
		series.Specials = &season
	} else {
		series.Seasons[strconv.Itoa(opts.Season)] = season
	}

	if err := series.Validate(); err != nil {
		return Series{}, err
	}
	return series, nil
}

func cleanSource(source string) string {
	return media.ParseMediaSource(source).String()
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

// Validate checks whether the series document is safe to persist.
func (s Series) Validate() error {
	if err := validateSeriesV1Schema(seriesToV1(s)); err != nil {
		return err
	}
	for seasonKey, season := range s.Seasons {
		seasonNumber, err := strconv.Atoi(seasonKey)
		if err != nil || seasonNumber < 1 {
			return fmt.Errorf("library: invalid season key %q", seasonKey)
		}
		if err := validateSeasonPaths(seasonNumber, season); err != nil {
			return err
		}
	}
	if s.Specials != nil {
		return validateSeasonPaths(0, *s.Specials)
	}
	return nil
}

func validateSeasonPaths(seasonNumber int, season Season) error {
	for episodeKey, episode := range season.Episodes {
		episodeNumber, err := strconv.Atoi(episodeKey)
		if err != nil || episodeNumber < 1 {
			return fmt.Errorf("library: invalid episode key %q in season %d", episodeKey, seasonNumber)
		}
		if _, err := layout.CleanSeriesRelPath(episode.Media.Path); err != nil {
			return fmt.Errorf("library: invalid media path for S%02dE%02d: %w", seasonNumber, episodeNumber, err)
		}
		for _, companion := range episode.Companions {
			if _, err := layout.CleanSeriesRelPath(companion.Path); err != nil {
				return fmt.Errorf("library: invalid companion path for S%02dE%02d: %w", seasonNumber, episodeNumber, err)
			}
		}
	}
	return nil
}
