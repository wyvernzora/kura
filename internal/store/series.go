// Package store owns Kura's persistent per-series library state.
package store

import (
	"encoding/json"
	"fmt"
	"sort"

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
	SchemaVersion     int      `json:"schemaVersion"`
	ID                string   `json:"id"`
	ProviderRefs      []string `json:"providerRefs"`
	PreferredProvider string   `json:"preferredProvider"`
	PreferredTitle    string   `json:"preferredTitle"`
	CanonicalTitle    string   `json:"canonicalTitle"`
	LastScanned       string   `json:"lastScanned,omitempty"`
	Notes             string   `json:"notes,omitempty"`
	Seasons           []Season `json:"seasons,omitempty"`
	Specials          *Season  `json:"specials,omitempty"`

	dirname string
}

func (s Series) MarshalJSON() ([]byte, error) {
	return json.Marshal(seriesToV1(s))
}

// Season stores local state for one regular season or the specials collection.
type Season struct {
	Number   int       `json:"number"`
	Notes    string    `json:"notes,omitempty"`
	Episodes []Episode `json:"episodes,omitempty"`
}

// Episode stores local state for one episode.
type Episode struct {
	Number     int             `json:"number"`
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
	for _, season := range s.Seasons {
		if season.Number < 1 {
			return fmt.Errorf("library: invalid season number %d", season.Number)
		}
		if err := validateSeasonPaths(season.Number, season); err != nil {
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

func (s Series) Season(number int) (*Season, bool) {
	if number == 0 {
		if s.Specials == nil {
			return nil, false
		}
		return s.Specials, true
	}
	for i := range s.Seasons {
		if s.Seasons[i].Number == number {
			return &s.Seasons[i], true
		}
	}
	return nil, false
}

func (s Series) LookupEpisode(seasonNumber int, episodeNumber int) (Episode, bool) {
	season, ok := s.Season(seasonNumber)
	if !ok {
		return Episode{}, false
	}
	episode, ok := season.Episode(episodeNumber)
	if !ok || episode.Media.Path == "" {
		return Episode{}, false
	}
	return *episode, true
}

func (s *Series) UpsertSeason(season Season) {
	if season.Number == 0 {
		s.Specials = &season
		return
	}
	for i := range s.Seasons {
		if s.Seasons[i].Number == season.Number {
			s.Seasons[i] = season
			return
		}
	}
	s.Seasons = append(s.Seasons, season)
	sort.Slice(s.Seasons, func(i, j int) bool {
		return s.Seasons[i].Number < s.Seasons[j].Number
	})
}

func (s Season) Episode(number int) (*Episode, bool) {
	for i := range s.Episodes {
		if s.Episodes[i].Number == number {
			return &s.Episodes[i], true
		}
	}
	return nil, false
}

func (s *Season) UpsertEpisode(episode Episode) {
	for i := range s.Episodes {
		if s.Episodes[i].Number == episode.Number {
			s.Episodes[i] = episode
			return
		}
	}
	s.Episodes = append(s.Episodes, episode)
	sort.Slice(s.Episodes, func(i, j int) bool {
		return s.Episodes[i].Number < s.Episodes[j].Number
	})
}

func validateSeasonPaths(seasonNumber int, season Season) error {
	for _, episode := range season.Episodes {
		if episode.Number < 1 {
			return fmt.Errorf("library: invalid episode number %d in season %d", episode.Number, seasonNumber)
		}
		if _, err := layout.CleanSeriesRelPath(episode.Media.Path); err != nil {
			return fmt.Errorf("library: invalid media path for S%02dE%02d: %w", seasonNumber, episode.Number, err)
		}
		for _, companion := range episode.Companions {
			if _, err := layout.CleanSeriesRelPath(companion.Path); err != nil {
				return fmt.Errorf("library: invalid companion path for S%02dE%02d: %w", seasonNumber, episode.Number, err)
			}
		}
	}
	return nil
}
