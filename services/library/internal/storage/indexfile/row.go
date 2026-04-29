package indexfile

import (
	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/response"
)

// SchemaVersion is the on-disk schema version stamped into the JSONL header.
// Bump when row-computation rules change in a way that invalidates rebuilt
// rows (e.g. specials start counting toward EpisodesAvailable). LoadCAS
// surfaces a schema mismatch via ErrSchemaMismatch so callers force a rebuild.
const SchemaVersion = 2

// Header is the JSONL header line. One per file, line 1. Empty libraries
// have just the header.
type Header struct {
	SchemaVersion int            `json:"$schema"`
	IndexAsOf     string         `json:"indexAsOf"`
	LastMutated   *coord.Mutator `json:"lastMutated,omitempty"`
}

// Row is the materialized view of one library entry. Persisted at one
// JSON object per line under the JSONL header. Untracked directories
// (no series.json) are persisted with Status="untracked" and no
// metadata; those rows still occupy a line so the read path can answer
// kura_list without a disk walk.
//
// All counts and rollups exclude specials (season 0); those rules live
// in the builder and are the single source of truth for what a row
// looks like for a given series.json.
type Row struct {
	Series   refs.Series   `json:"series"`
	Metadata refs.Metadata `json:"metadata,omitempty"`

	Title          string `json:"title"`
	CanonicalTitle string `json:"canonicalTitle,omitempty"`

	Status response.ListStatus `json:"status"`
	Staged bool                `json:"staged,omitempty"`

	SeasonsAvailable  int `json:"seasonsAvailable,omitempty"`
	SeasonCount       int `json:"seasonCount,omitempty"`
	EpisodesAvailable int `json:"episodesAvailable,omitempty"`
	EpisodeCount      int `json:"episodeCount,omitempty"`

	Resolutions []string `json:"resolutions,omitempty"`
	Sources     []string `json:"sources,omitempty"`

	LastScanned string `json:"lastScanned,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`

	Error string `json:"error,omitempty"`
}
