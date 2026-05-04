package response

import "github.com/wyvernzora/kura/internal/domain/refs"

// ListStatus is the rolled-up state of one series in the library list.
// Library-level (vs episode-level Status above): does the series have
// any tracked metadata, are any episodes missing, are any unaired, etc.
type ListStatus string

const (
	// ListStatusUntracked: directory under the library root has no
	// .kura/series.json (Kura does not manage it).
	ListStatusUntracked ListStatus = "untracked"

	// ListStatusComplete: every aired episode has present media, no
	// pending air dates, no missing entries.
	ListStatusComplete ListStatus = "complete"

	// ListStatusIncomplete: at least one aired episode is missing
	// active media, or the series has no episodes at all.
	ListStatusIncomplete ListStatus = "incomplete"

	// ListStatusAiring: every aired episode is present, but at least
	// one upcoming episode has not yet aired.
	ListStatusAiring ListStatus = "airing"

	// ListStatusError: the row could not be computed (parse error,
	// load error). The Error field carries the message.
	ListStatusError ListStatus = "error"
)

// ListRow is one row in the library list response. Counts and quality
// rollups exclude specials (season 0) — they don't factor into series
// observed state. Resolutions and Sources are the distinct values
// across non-special episodes with active records, sorted high-
// quality-first.
//
// SeasonsAvailable / EpisodesAvailable count slots populated by an
// active record. SeasonCount / EpisodeCount are the totals. Renderers
// surface them as "available/total".
type ListRow struct {
	Status            ListStatus    `json:"status"`
	Staged            bool          `json:"staged,omitempty"`
	Title             string        `json:"title"`
	CanonicalTitle    string        `json:"canonicalTitle,omitempty"`
	SeasonsAvailable  int           `json:"seasonsAvailable"`
	SeasonCount       int           `json:"seasonCount"`
	EpisodesAvailable int           `json:"episodesAvailable"`
	EpisodeCount      int           `json:"episodeCount"`
	MetadataRef       refs.Metadata `json:"metadataRef,omitempty"`
	Resolutions       []string      `json:"resolutions,omitempty"`
	Sources           []string      `json:"sources,omitempty"`
	LastScanned       string        `json:"lastScanned,omitempty"`
	Error             string        `json:"error,omitempty"`
}

// ListResult is the full library-list response. Rows are sorted by
// the on-disk directory name (computed at workflow time, not surfaced).
type ListResult struct {
	Rows []ListRow `json:"rows"`
}
