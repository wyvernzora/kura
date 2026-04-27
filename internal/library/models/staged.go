package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	layout "github.com/wyvernzora/kura/internal/fsroot"
)

const (
	StagedSchemaVersion = 1
)

// StagedEpisode stores an explicitly admitted external media file before Kura
// applies it to active series metadata and filesystem layout.
type StagedEpisode struct {
	Season int `json:"season"`
	Number int `json:"number"`

	Episode
}

// Staged is the persistent .kura/staged.json document for one local series.
type Staged struct {
	SchemaVersion int             `json:"schemaVersion"`
	Entries       []StagedEpisode `json:"entries,omitempty"`

	dirname string
}

func (i Staged) MarshalJSON() ([]byte, error) {
	return json.Marshal(stagedDocumentToV1(i))
}

func StagedPath(seriesDir string) string {
	return layout.StagedMetadataPath(seriesDir)
}

func (i Staged) Validate() error {
	if err := validateStagedV1Schema(stagedDocumentToV1(i)); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, staged := range i.Entries {
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

func (i Staged) IsEmpty() bool {
	return len(i.Entries) == 0
}

func (i Staged) Lookup(season int, number int) (StagedEpisode, int, bool) {
	for index, staged := range i.Entries {
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
