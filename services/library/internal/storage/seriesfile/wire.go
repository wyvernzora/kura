package seriesfile

import (
	"encoding/json"
	"fmt"
)

const currentSchemaVersion = 3

// seriesV3 is the wire shape for the current schema. Operators on
// pre-v3 files (v1 or v2) must re-scan to upgrade — the v2-tolerant
// decode path was removed once every series had been re-scanned.
type seriesV3 struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	MetadataRef    string               `json:"metadataRef"`
	PreferredTitle string               `json:"preferredTitle,omitempty"`
	CanonicalTitle string               `json:"canonicalTitle,omitempty"`
	DateAdded      string               `json:"dateAdded,omitempty"`
	LastScanned    string               `json:"lastScanned"`
	Ordering       string               `json:"ordering,omitempty"`
	Artwork        artworkV2            `json:"artwork"`
	Episodes       map[string]episodeV2 `json:"episodes"`
	StagedTrash    []stagedTrashEntryV1 `json:"stagedTrash"`
	StagedExtras   []stagedExtraEntryV1 `json:"stagedExtras"`
	UserAliases    []string             `json:"userAliases,omitempty"`
	SearchKey      string               `json:"searchKey"`
	InProgress     *holderV1            `json:"in_progress,omitempty"`
	LastMutated    mutatorV1            `json:"last_mutated"`
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
	Attrs      map[string]string   `json:"attrs,omitempty"`
}

type companionRecordV1 struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}

// encode emits the v3 wire shape regardless of how the file was loaded.
// Lazy migration: a v2 file gets upgraded to v3 the first time any
// workflow saves it. New fields (translated titles / user aliases /
// search key) are empty by default; populated by scan + alias
// mutations.
func encode(s seriesV3) ([]byte, error) {
	normalizeWire(&s)
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

func normalizeWire(s *seriesV3) {
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
	// Always emit empty arrays for the staged-* slots — schema marks
	// them required so a nil slice would render as `null` and fail
	// validation. UserAliases stays optional (omitempty).
	if s.StagedTrash == nil {
		s.StagedTrash = []stagedTrashEntryV1{}
	}
	if s.StagedExtras == nil {
		s.StagedExtras = []stagedExtraEntryV1{}
	}
	if len(s.UserAliases) == 0 {
		s.UserAliases = nil
	}
}

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

// decode parses a v3 series.json. Pre-v3 files (v1, v2) are rejected
// — operators on legacy files must re-scan to upgrade. v4+ rejected
// as forward-incompatible.
func decode(data []byte) (seriesV3, error) {
	return decodeWire(data, true)
}

func decodeWire(data []byte, validate bool) (seriesV3, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return seriesV3{}, fmt.Errorf("seriesfile: decode: %w", err)
	}
	if header.SchemaVersion != currentSchemaVersion {
		return seriesV3{}, fmt.Errorf("seriesfile: unsupported schemaVersion %d", header.SchemaVersion)
	}
	if validate {
		if err := validateSeries(currentSchemaVersion, data); err != nil {
			return seriesV3{}, err
		}
	}
	var s seriesV3
	if err := json.Unmarshal(data, &s); err != nil {
		return seriesV3{}, fmt.Errorf("seriesfile: decode: %w", err)
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
