package seriesfile

import (
	"encoding/json"
	"fmt"
)

const currentSchemaVersion = 1

type seriesV1 struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	MetadataRef    string               `json:"metadataRef"`
	PreferredTitle string               `json:"preferredTitle,omitempty"`
	CanonicalTitle string               `json:"canonicalTitle,omitempty"`
	LastScanned    string               `json:"lastScanned,omitempty"`
	Episodes       map[string]episodeV1 `json:"episodes"`
	InProgress     *holderV1            `json:"in_progress,omitempty"`
	LastMutated    *mutatorV1           `json:"last_mutated,omitempty"`
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

type episodeV1 struct {
	AirDate string         `json:"airDate,omitempty"`
	Active  *mediaRecordV1 `json:"active,omitempty"`
	Staged  *mediaRecordV1 `json:"staged,omitempty"`
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

func encode(s seriesV1) ([]byte, error) {
	s.SchemaVersion = currentSchemaVersion
	if s.Episodes == nil {
		s.Episodes = map[string]episodeV1{}
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
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := validateSeries(data); err != nil {
		return nil, err
	}
	return data, nil
}

type schemaHeader struct {
	SchemaVersion int `json:"schemaVersion"`
}

func decode(data []byte) (seriesV1, error) {
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return seriesV1{}, fmt.Errorf("seriesfile: decode: %w", err)
	}
	if header.SchemaVersion != currentSchemaVersion {
		return seriesV1{}, fmt.Errorf("seriesfile: unsupported schemaVersion %d", header.SchemaVersion)
	}
	if err := validateSeries(data); err != nil {
		return seriesV1{}, err
	}
	var s seriesV1
	if err := json.Unmarshal(data, &s); err != nil {
		return seriesV1{}, fmt.Errorf("seriesfile: decode: %w", err)
	}
	if s.Episodes == nil {
		s.Episodes = map[string]episodeV1{}
	}
	return s, nil
}
