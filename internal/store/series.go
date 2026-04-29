// Package store owns Kura's persistent per-series library state.
package store

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	media "github.com/wyvernzora/kura/internal/domain"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/store/schema"
)

const SeriesSchemaVersion = 1

// Series is the persistent .kura/series.json document for one local series.
//
// It stores local library facts only. Live source metadata, such as episode
// titles and air dates, belongs in metadata read views and is intentionally not
// persisted here.
type Series struct {
	SchemaVersion  int      `json:"schemaVersion"`
	MetadataRef    string   `json:"metadataRef"`
	PreferredTitle string   `json:"preferredTitle"`
	CanonicalTitle string   `json:"canonicalTitle"`
	LastScanned    string   `json:"lastScanned,omitempty"`
	Notes          string   `json:"notes,omitempty"`
	Seasons        []Season `json:"seasons,omitempty"`

	dirname string
}

// Season stores local state for one season. Season 0 represents specials.
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

// MarshalJSON ensures Companions serializes as `[]` rather than `null` so the
// schema's required-field invariant holds.
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

// MediaInfo aliases the canonical MediaInfo type from the domain layer so
// callers within store can refer to it without importing domain.
type MediaInfo = media.MediaInfo

// SeriesMetadataPath returns the metadata path for a series directory.
func SeriesMetadataPath(seriesDir string) string {
	return fsroot.SeriesMetadataPath(seriesDir)
}

func (s Series) Validate() error {
	schemaSeries, err := schema.SeriesV1()
	if err != nil {
		return err
	}
	if err := schema.ValidateValue(schemaSeries, s); err != nil {
		return fmt.Errorf("library: validate series: %w", err)
	}
	for _, season := range s.Seasons {
		if season.Number < 0 {
			return fmt.Errorf("library: invalid season number %d", season.Number)
		}
		if err := validateSeasonPaths(season.Number, season); err != nil {
			return err
		}
		if err := validateUniqueEpisodes(season.Number, season); err != nil {
			return err
		}
	}
	if err := validateUniqueSeasons(s.Seasons); err != nil {
		return err
	}
	return nil
}

func (s Series) Season(number int) (*Season, bool) {
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
		if _, err := fsroot.CleanSeriesRelPath(episode.Media.Path); err != nil {
			return fmt.Errorf("library: invalid media path for S%02dE%02d: %w", seasonNumber, episode.Number, err)
		}
		for _, companion := range episode.Companions {
			if _, err := fsroot.CleanSeriesRelPath(companion.Path); err != nil {
				return fmt.Errorf("library: invalid companion path for S%02dE%02d: %w", seasonNumber, episode.Number, err)
			}
		}
	}
	return nil
}

func validateUniqueSeasons(seasons []Season) error {
	seen := map[int]struct{}{}
	for _, season := range seasons {
		if _, exists := seen[season.Number]; exists {
			return fmt.Errorf("library: duplicate season number %d", season.Number)
		}
		seen[season.Number] = struct{}{}
	}
	return nil
}

func validateUniqueEpisodes(seasonNumber int, season Season) error {
	seen := map[int]struct{}{}
	for _, episode := range season.Episodes {
		if _, exists := seen[episode.Number]; exists {
			return DuplicateEpisodeNumberError{Season: seasonNumber, Episode: episode.Number}
		}
		seen[episode.Number] = struct{}{}
	}
	return nil
}

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

func decodeSeries(data []byte, path string) (Series, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Series{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	if header.SchemaVersion != SeriesSchemaVersion {
		return Series{}, fmt.Errorf("library: unsupported series schemaVersion %d", header.SchemaVersion)
	}
	schemaSeries, err := schema.SeriesV1()
	if err != nil {
		return Series{}, err
	}
	if err := schema.ValidateBytes(schemaSeries, data); err != nil {
		return Series{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	var series Series
	if err := json.Unmarshal(data, &series); err != nil {
		return Series{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	for _, season := range series.Seasons {
		if err := validateUniqueEpisodes(season.Number, season); err != nil {
			return Series{}, fmt.Errorf("library: validate %s: %w", path, err)
		}
	}
	if err := validateUniqueSeasons(series.Seasons); err != nil {
		return Series{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	canonicalizeSeries(&series)
	return series, nil
}

func encodeSeries(w io.Writer, series Series) error {
	if series.SchemaVersion != SeriesSchemaVersion {
		return fmt.Errorf("library: unsupported series schemaVersion %d", series.SchemaVersion)
	}
	canonicalizeSeries(&series)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(series)
}

func canonicalizeSeries(s *Series) {
	for i := range s.Seasons {
		canonicalizeSeason(&s.Seasons[i])
	}
	sort.Slice(s.Seasons, func(i, j int) bool { return s.Seasons[i].Number < s.Seasons[j].Number })
}

func canonicalizeSeason(s *Season) {
	for i := range s.Episodes {
		s.Episodes[i].Media.Source = media.ParseMediaSource(s.Episodes[i].Media.Source).String()
		if s.Episodes[i].Companions == nil {
			s.Episodes[i].Companions = []CompanionFile{}
		}
	}
	sort.Slice(s.Episodes, func(i, j int) bool { return s.Episodes[i].Number < s.Episodes[j].Number })
}
