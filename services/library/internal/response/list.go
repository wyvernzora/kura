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
	// Series-level poster artwork URLs lifted from series.json. Empty
	// strings when the metadata provider had no poster for the series
	// or the row predates poster surfacing — clients should fall back
	// to a placeholder when both are empty.
	PosterURL          string `json:"posterUrl,omitempty"`
	PosterThumbnailURL string `json:"posterThumbnailUrl,omitempty"`
	LastScanned        string `json:"lastScanned,omitempty"`
	// SearchKey is the per-row folded search blob the client feeds
	// into local fuzzy search. Server-side data only — never displayed.
	// Empty when no candidate produces a token (rare; typically a
	// JP-only library that never ran a v3-aware scan).
	SearchKey string `json:"searchKey,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ListResult is the full library-list response. Rows are sorted by
// title (lower-cased) and tie-broken on series ref.
//
// NextCursor is non-empty when MaxResults capped the page; pass it back
// as the next request's Cursor to fetch the following page. DataChanged
// is true when the index changed between pages — clients should re-render
// from the start of the current page if they care about strict ordering.
type ListResult struct {
	Rows        []ListRow `json:"rows"`
	NextCursor  string    `json:"nextCursor,omitempty"`
	DataChanged bool      `json:"dataChanged,omitempty"`
}
