package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/wyvernzora/kura/internal/fsroot"
	"github.com/wyvernzora/kura/internal/store/schema"
)

const TrashSchemaVersion = 1

// TrashedEpisode stores a replaced local episode until reconciliation moves it
// out of the active series layout.
type TrashedEpisode struct {
	ID     string `json:"id"`
	Season int    `json:"season"`
	Number int    `json:"number"`

	Episode
}

// MarshalJSON serializes TrashedEpisode as a flat document, overriding the
// embedded Episode's MarshalJSON which would otherwise hide the outer fields.
func (t TrashedEpisode) MarshalJSON() ([]byte, error) {
	out := trashEntryWire{
		ID:         t.ID,
		Season:     t.Season,
		Number:     t.Number,
		Media:      t.Media,
		Companions: t.Companions,
	}
	if out.Companions == nil {
		out.Companions = []CompanionFile{}
	}
	return json.Marshal(out)
}

// UnmarshalJSON decodes the flat wire shape and mirrors Number into the
// embedded Episode so callers reading t.Episode see consistent state.
func (t *TrashedEpisode) UnmarshalJSON(data []byte) error {
	var wire trashEntryWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	t.ID = wire.ID
	t.Season = wire.Season
	t.Number = wire.Number
	t.Episode = Episode{
		Number:     wire.Number,
		Media:      wire.Media,
		Companions: wire.Companions,
	}
	if t.Companions == nil {
		t.Companions = []CompanionFile{}
	}
	return nil
}

type trashEntryWire struct {
	ID         string          `json:"id"`
	Season     int             `json:"season"`
	Number     int             `json:"number"`
	Media      MediaFile       `json:"media"`
	Companions []CompanionFile `json:"companions"`
}

// Trash is the persistent .kura/trash.json document for one local series.
type Trash struct {
	SchemaVersion int              `json:"schemaVersion"`
	Entries       []TrashedEpisode `json:"entries,omitempty"`

	dirname string
}

// TrashPath returns the metadata path for a series directory's trash document.
func TrashPath(seriesDir string) string {
	return fsroot.TrashMetadataPath(seriesDir)
}

func NewTrashedEpisode(season int, number int, episode Episode) TrashedEpisode {
	episode.Number = number
	return TrashedEpisode{
		ID:      ulid.Make().String(),
		Season:  season,
		Number:  number,
		Episode: episode,
	}
}

func (t Trash) Validate() error {
	schemaTrash, err := schema.TrashV1()
	if err != nil {
		return err
	}
	if err := schema.ValidateValue(schemaTrash, t); err != nil {
		return fmt.Errorf("library: validate trash: %w", err)
	}
	ids := map[string]struct{}{}
	for _, trashed := range t.Entries {
		if strings.TrimSpace(trashed.ID) == "" {
			return errors.New("library: trash id is required")
		}
		if _, exists := ids[trashed.ID]; exists {
			return fmt.Errorf("library: duplicate trash id %q", trashed.ID)
		}
		ids[trashed.ID] = struct{}{}
		if trashed.Season < 0 {
			return fmt.Errorf("library: invalid trash season %d", trashed.Season)
		}
		if trashed.Number < 1 {
			return fmt.Errorf("library: invalid trash episode %d", trashed.Number)
		}
		if _, err := fsroot.CleanRelPathAllowingKura(trashed.Media.Path); err != nil {
			return fmt.Errorf("library: invalid trashed media path for %s: %w", trashed.ID, err)
		}
		for _, companion := range trashed.Companions {
			if _, err := fsroot.CleanRelPathAllowingKura(companion.Path); err != nil {
				return fmt.Errorf("library: invalid trashed companion path for %s: %w", trashed.ID, err)
			}
		}
	}
	return nil
}

func (t Trash) IsEmpty() bool {
	return len(t.Entries) == 0
}

func decodeTrash(data []byte, path string) (Trash, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return Trash{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	if header.SchemaVersion != TrashSchemaVersion {
		return Trash{}, fmt.Errorf("library: unsupported trash schemaVersion %d", header.SchemaVersion)
	}
	schemaTrash, err := schema.TrashV1()
	if err != nil {
		return Trash{}, err
	}
	if err := schema.ValidateBytes(schemaTrash, data); err != nil {
		return Trash{}, fmt.Errorf("library: validate %s: %w", path, err)
	}
	var trash Trash
	if err := json.Unmarshal(data, &trash); err != nil {
		return Trash{}, fmt.Errorf("library: decode %s: %w", path, err)
	}
	return trash, nil
}

func encodeTrash(w io.Writer, trash Trash) error {
	if trash.SchemaVersion != TrashSchemaVersion {
		return fmt.Errorf("library: unsupported trash schemaVersion %d", trash.SchemaVersion)
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(trash)
}
