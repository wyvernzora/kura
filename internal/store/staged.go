package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/store/schema"
)

const StagedSchemaVersion = 1

// StagedEpisode stores an explicitly admitted external media file before Kura
// applies it to active series metadata and filesystem layout.
type StagedEpisode struct {
	Season int `json:"season"`
	Number int `json:"number"`

	Episode
}

// MarshalJSON serializes StagedEpisode as a flat document so the outer Season
// and Number fields appear alongside the embedded Episode's media and
// companion fields. Required to override the embedded Episode's MarshalJSON
// which would otherwise hide the outer fields.
func (s StagedEpisode) MarshalJSON() ([]byte, error) {
	out := stagedEntryWire{
		Season:     s.Season,
		Number:     s.Number,
		Media:      s.Media,
		Companions: s.Companions,
	}
	if out.Companions == nil {
		out.Companions = []CompanionFile{}
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes the flat wire shape and mirrors Number into the
// embedded Episode so callers that read s.Episode see consistent state.
func (s *StagedEpisode) UnmarshalJSON(data []byte) error {
	var wire stagedEntryWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	s.Season = wire.Season
	s.Number = wire.Number
	s.Episode = Episode{
		Number:     wire.Number,
		Media:      wire.Media,
		Companions: wire.Companions,
	}
	if s.Companions == nil {
		s.Companions = []CompanionFile{}
	}
	return nil
}

type stagedEntryWire struct {
	Season     int             `json:"season"`
	Number     int             `json:"number"`
	Media      MediaFile       `json:"media"`
	Companions []CompanionFile `json:"companions"`
}

// Staged is the persistent .kura/staged.json document for one local series.
type Staged struct {
	SchemaVersion int             `json:"schemaVersion"`
	Entries       []StagedEpisode `json:"entries,omitempty"`

	dirname string
}

// StagedPath returns the metadata path for a series directory's staged document.
func StagedPath(seriesDir string) string {
	return fsroot.StagedMetadataPath(seriesDir)
}

func (s Staged) Validate() error {
	schemaStaged, err := schema.StagedV1()
	if err != nil {
		return err
	}
	if err := schema.ValidateValue(schemaStaged, s); err != nil {
		return fmt.Errorf("library: validate staged: %w", err)
	}
	seen := map[string]struct{}{}
	for _, staged := range s.Entries {
		if staged.Season < 0 {
			return fmt.Errorf("library: invalid staged season %d", staged.Season)
		}
		if staged.Number < 1 {
			return fmt.Errorf("library: invalid staged episode %d", staged.Number)
		}
		key := fmt.Sprintf("%d:%d", staged.Season, staged.Number)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("library: duplicate staged episode S%02dE%02d", staged.Season, staged.Number)
		}
		seen[key] = struct{}{}
		if err := validateAbsolutePath(staged.Media.Path); err != nil {
			return fmt.Errorf("library: invalid staged media path for S%02dE%02d: %w", staged.Season, staged.Number, err)
		}
		for _, companion := range staged.Companions {
			if err := validateAbsolutePath(companion.Path); err != nil {
				return fmt.Errorf("library: invalid staged companion path for S%02dE%02d: %w", staged.Season, staged.Number, err)
			}
		}
	}
	return nil
}

func (s Staged) IsEmpty() bool {
	return len(s.Entries) == 0
}

func (s Staged) Lookup(season int, number int) (StagedEpisode, int, bool) {
	for index, staged := range s.Entries {
		if staged.Season == season && staged.Number == number {
			return staged, index, true
		}
	}
	return StagedEpisode{}, -1, false
}

func validateAbsolutePath(path string) error {
	if path == "" {
		return errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path %q must be absolute", path)
	}
	return nil
}

func decodeStaged(data []byte, path string) (Staged, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Staged{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	if header.SchemaVersion != StagedSchemaVersion {
		return Staged{}, fmt.Errorf("library: unsupported staged schemaVersion %d", header.SchemaVersion)
	}
	schemaStaged, err := schema.StagedV1()
	if err != nil {
		return Staged{}, err
	}
	if err := schema.ValidateBytes(schemaStaged, data); err != nil {
		return Staged{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	var staged Staged
	if err := json.Unmarshal(data, &staged); err != nil {
		return Staged{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	return staged, nil
}

func encodeStaged(w io.Writer, staged Staged) error {
	if staged.SchemaVersion != StagedSchemaVersion {
		return fmt.Errorf("library: unsupported staged schemaVersion %d", staged.SchemaVersion)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(staged)
}
