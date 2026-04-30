package wire

const CurrentSchemaVersion = 1

type Series struct {
	SchemaVersion int                `json:"schemaVersion"`
	MetadataRef   string             `json:"metadataRef"`
	LastScanned   string             `json:"lastScanned,omitempty"`
	Episodes      map[string]Episode `json:"episodes,omitempty"`
}

type Episode struct {
	Season  int          `json:"season"`
	Episode int          `json:"episode"`
	AirDate string       `json:"airDate,omitempty"`
	Active  *MediaRecord `json:"active,omitempty"`
	Staged  *MediaRecord `json:"staged,omitempty"`
}

type MediaRecord struct {
	Path       string            `json:"path"`
	Source     string            `json:"source"`
	Resolution string            `json:"resolution,omitempty"`
	Codec      string            `json:"codec,omitempty"`
	Size       int64             `json:"size"`
	MTime      string            `json:"mtime"`
	Companions []CompanionRecord `json:"companions"`
}

type CompanionRecord struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}
