// Package store owns Kura's persistent per-series library state.
package store

import (
	"encoding/json"
	"fmt"
	"strconv"

	media "github.com/wyvernzora/kura/internal/domain"
	layout "github.com/wyvernzora/kura/internal/fsroot"
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

// SeriesPath returns the metadata path for a series directory.
func SeriesPath(seriesDir string) string {
	return layout.SeriesMetadataPath(seriesDir)
}

func cleanSource(source string) string {
	return media.ParseMediaSource(source).String()
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
		if err := validateSeasonPaths(0, *s.Specials); err != nil {
			return err
		}
	}
	return nil
}

func (s Series) LookupEpisode(seasonNumber int, episodeNumber int) (Episode, bool) {
	if seasonNumber == 0 {
		if s.Specials == nil || s.Specials.Episodes == nil {
			return Episode{}, false
		}
		episode, ok := s.Specials.Episodes[strconv.Itoa(episodeNumber)]
		return episode, ok && episode.Media.Path != ""
	}
	season, ok := s.Seasons[strconv.Itoa(seasonNumber)]
	if !ok || season.Episodes == nil {
		return Episode{}, false
	}
	episode, ok := season.Episodes[strconv.Itoa(episodeNumber)]
	return episode, ok && episode.Media.Path != ""
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
