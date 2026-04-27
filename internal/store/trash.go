package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"
	layout "github.com/wyvernzora/kura/internal/fsroot"
)

const (
	TrashSchemaVersion = 1
)

// TrashedEpisode stores a replaced local episode until reconciliation moves it
// out of the active series layout.
type TrashedEpisode struct {
	ID     string `json:"id"`
	Season int    `json:"season"`
	Number int    `json:"number"`

	Episode
}

// Trash is the persistent .kura/trash.json document for one local series.
type Trash struct {
	SchemaVersion int              `json:"schemaVersion"`
	Entries       []TrashedEpisode `json:"entries,omitempty"`

	dirname string
}

func (t Trash) MarshalJSON() ([]byte, error) {
	return json.Marshal(trashDocumentToV1(t))
}

func TrashPath(seriesDir string) string {
	return layout.TrashMetadataPath(seriesDir)
}

func NewTrashedEpisode(season int, number int, episode Episode) TrashedEpisode {
	return TrashedEpisode{
		ID:      ulid.Make().String(),
		Season:  season,
		Number:  number,
		Episode: episode,
	}
}

func (t Trash) Validate() error {
	if err := validateTrashV1Schema(trashDocumentToV1(t)); err != nil {
		return err
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
		if _, err := cleanTrashRelPath(trashed.Media.Path); err != nil {
			return fmt.Errorf("library: invalid trashed media path for %s: %w", trashed.ID, err)
		}
		for _, companion := range trashed.Companions {
			if _, err := cleanTrashRelPath(companion.Path); err != nil {
				return fmt.Errorf("library: invalid trashed companion path for %s: %w", trashed.ID, err)
			}
		}
	}
	return nil
}

func (t Trash) IsEmpty() bool {
	return len(t.Entries) == 0
}

func cleanTrashRelPath(path string) (string, error) {
	relPath, err := layout.CleanRelPathAllowingKura(path)
	if err != nil {
		return "", err
	}
	return relPath, nil
}
