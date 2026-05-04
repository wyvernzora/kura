package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// Show is workflow.Show's full response: persisted series metadata
// joined with derived per-episode status and filesystem-issue lists.
type Show struct {
	MetadataRef    refs.Metadata `json:"metadataRef"`
	Ref            refs.Series   `json:"ref"`
	Root           string        `json:"root"`
	LastScanned    string        `json:"lastScanned,omitempty"`
	PreferredTitle string        `json:"preferredTitle"`
	CanonicalTitle string        `json:"canonicalTitle,omitempty"`
	Seasons        []SeasonShow  `json:"seasons"`
}

// SeasonShow groups episode rows by season number. Number 0 is specials.
type SeasonShow struct {
	Number   int           `json:"number"`
	Episodes []EpisodeShow `json:"episodes"`
}

// EpisodeShow is one episode row with computed status, the active and
// staged media records (if any), and any filesystem inconsistencies.
type EpisodeShow struct {
	Episode         refs.Episode `json:"episode"`
	Aired           string       `json:"aired,omitempty"`
	Status          Status       `json:"status"`
	Active          *MediaShow   `json:"active,omitempty"`
	Staged          *MediaShow   `json:"staged,omitempty"`
	Inconsistencies []Issue      `json:"inconsistencies,omitempty"`
}

// MediaShow is one media file's display fields for the show view. Paths
// inside the series root are series-relative slash form; paths outside
// (e.g. staged-from-inbox files) stay absolute.
type MediaShow struct {
	Source     string          `json:"source"`
	Resolution string          `json:"resolution,omitempty"`
	Codec      string          `json:"codec,omitempty"`
	Size       int64           `json:"size"`
	File       string          `json:"file"`
	Companions []CompanionShow `json:"companions"`
}

type CompanionShow struct {
	Path     string `json:"path"`
	Role     string `json:"role,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Size     int64  `json:"size"`
	MTime    string `json:"mtime"`
}
