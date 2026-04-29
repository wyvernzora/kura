package seriesfile

import (
	"encoding/json"
	"fmt"
)

const currentSchemaVersion = 2

// seriesV2 is the wire shape for the current schema. Additive over the
// retired v1 form: same fields plus per-episode title pair (rolled
// into episodeV2) and series-level Artwork. v1 files are no longer
// readable; operators on a v1 file must re-scan to upgrade.
type seriesV2 struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	MetadataRef    string               `json:"metadataRef"`
	PreferredTitle string               `json:"preferredTitle,omitempty"`
	CanonicalTitle string               `json:"canonicalTitle,omitempty"`
	LastScanned    string               `json:"lastScanned,omitempty"`
	Ordering       string               `json:"ordering,omitempty"`
	Artwork        *artworkV2           `json:"artwork,omitempty"`
	Episodes       map[string]episodeV2 `json:"episodes"`
	StagedTrash    []stagedTrashEntryV1 `json:"stagedTrash,omitempty"`
	StagedExtras   []stagedExtraEntryV1 `json:"stagedExtras,omitempty"`
	InProgress     *holderV1            `json:"in_progress,omitempty"`
	LastMutated    *mutatorV1           `json:"last_mutated,omitempty"`
}

type artworkV2 struct {
	Poster *posterV2 `json:"poster,omitempty"`
}

type posterV2 struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
	Language     string `json:"language,omitempty"`
}

type episodeV2 struct {
	AirDate        string         `json:"airDate,omitempty"`
	PreferredTitle string         `json:"preferredTitle,omitempty"`
	CanonicalTitle string         `json:"canonicalTitle,omitempty"`
	Active         *mediaRecordV1 `json:"active,omitempty"`
	Staged         *mediaRecordV1 `json:"staged,omitempty"`
}

type stagedTrashEntryV1 struct {
	ID         string              `json:"id"`
	Path       string              `json:"path"`
	Size       int64               `json:"size"`
	MTime      string              `json:"mtime"`
	AddedAt    string              `json:"addedAt"`
	Companions []companionRecordV1 `json:"companions"`
}

type stagedExtraEntryV1 struct {
	ID      string `json:"id"`
	Season  int    `json:"season"`
	Path    string `json:"path"`
	Prefix  string `json:"prefix,omitempty"`
	IsDir   bool   `json:"isDir,omitempty"`
	AddedAt string `json:"addedAt"`
}

type holderV1 struct {
	Op      string `json:"op"`
	Token   string `json:"token,omitempty"`
	PID     int    `json:"pid"`
	Host    string `json:"host"`
	Started string `json:"started"`
}

type mutatorV1 struct {
	Op   string `json:"op"`
	PID  int    `json:"pid"`
	Host string `json:"host"`
	At   string `json:"at"`
}

type mediaRecordV1 struct {
	Path       string              `json:"path"`
	Source     string              `json:"source"`
	Resolution string              `json:"resolution,omitempty"`
	Codec      string              `json:"codec,omitempty"`
	Size       int64               `json:"size"`
	MTime      string              `json:"mtime"`
	Companions []companionRecordV1 `json:"companions"`
}

type companionRecordV1 struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

// encode emits the v2 wire shape regardless of how the file was loaded.
// Lazy migration: a v1 file gets upgraded to v2 the first time any
// workflow saves it. New fields (poster, per-episode titles) are
// nil/empty by default; populated by scan when the provider returns
// the data.
func encode(s seriesV2) ([]byte, error) {
	s.SchemaVersion = currentSchemaVersion
	if s.Episodes == nil {
		s.Episodes = map[string]episodeV2{}
	}
	for key, episode := range s.Episodes {
		if episode.Active != nil && episode.Active.Companions == nil {
			episode.Active.Companions = []companionRecordV1{}
		}
		if episode.Staged != nil && episode.Staged.Companions == nil {
			episode.Staged.Companions = []companionRecordV1{}
		}
		s.Episodes[key] = episode
	}
	for i := range s.StagedTrash {
		if s.StagedTrash[i].Companions == nil {
			s.StagedTrash[i].Companions = []companionRecordV1{}
		}
	}
	// Drop empty arrays before marshal so omitempty keeps them off disk.
	if len(s.StagedTrash) == 0 {
		s.StagedTrash = nil
	}
	if len(s.StagedExtras) == 0 {
		s.StagedExtras = nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := validateSeries(currentSchemaVersion, data); err != nil {
		return nil, err
	}
	return data, nil
}

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

// decode parses a v2 series.json. v1 files are no longer accepted —
// the v2 schema has been fully enforced; operators on legacy files
// must re-scan to upgrade. v3+ rejected as forward-incompatible.
func decode(data []byte) (seriesV2, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return seriesV2{}, fmt.Errorf("seriesfile: decode: %w", err)
	}
	if header.SchemaVersion != currentSchemaVersion {
		return seriesV2{}, fmt.Errorf("seriesfile: unsupported schemaVersion %d", header.SchemaVersion)
	}
	if err := validateSeries(currentSchemaVersion, data); err != nil {
		return seriesV2{}, err
	}
	var s seriesV2
	if err := json.Unmarshal(data, &s); err != nil {
		return seriesV2{}, fmt.Errorf("seriesfile: decode: %w", err)
	}
	if s.Episodes == nil {
		s.Episodes = map[string]episodeV2{}
	}
	if s.StagedTrash == nil {
		s.StagedTrash = []stagedTrashEntryV1{}
	}
	if s.StagedExtras == nil {
		s.StagedExtras = []stagedExtraEntryV1{}
	}
	return s, nil
}
