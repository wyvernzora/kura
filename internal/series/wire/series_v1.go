package wire

import "path/filepath"

const CurrentSchemaVersion = 1

const (
	KuraDir        = ".kura"
	SeriesFileName = "series.json"
)

func SeriesMetadataPath(seriesDir string) string {
	return filepath.Join(seriesDir, KuraDir, SeriesFileName)
}

type SeriesV1 struct {
	SchemaVersion int                  `json:"schemaVersion"`
	MetadataRef   string               `json:"metadataRef"`
	LastScanned   string               `json:"lastScanned,omitempty"`
	Episodes      map[string]EpisodeV1 `json:"episodes,omitempty"`
}

type EpisodeV1 struct {
	AirDate string         `json:"airDate,omitempty"`
	Active  *MediaRecordV1 `json:"active,omitempty"`
	Staged  *MediaRecordV1 `json:"staged,omitempty"`
}

type MediaRecordV1 struct {
	Path       string              `json:"path"`
	Source     string              `json:"source"`
	Resolution string              `json:"resolution,omitempty"`
	Codec      string              `json:"codec,omitempty"`
	Size       int64               `json:"size"`
	MTime      string              `json:"mtime"`
	Companions []CompanionRecordV1 `json:"companions"`
}

type CompanionRecordV1 struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}
