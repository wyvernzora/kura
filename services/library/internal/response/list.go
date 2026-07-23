package response

import "github.com/wyvernzora/kura/services/library/internal/domain/refs"

// ListStatus is the rolled-up state of one series in the library list.
// Library-level (vs episode-level Status above): does the series have
// any tracked metadata, are any episodes missing, are any unaired, etc.
type ListStatus string

const (
	// ListStatusUntracked: directory under the library root has no
	// .kura/series.json (Kura does not manage it).
	ListStatusUntracked ListStatus = "untracked"

	// ListStatusComplete: every currently actionable episode has active
	// or staged media. Pending-only series are complete because there is
	// no missing media to act on yet.
	ListStatusComplete ListStatus = "complete"

	// ListStatusIncomplete: at least one aired episode is missing
	// active media, or the series has no episodes at all.
	ListStatusIncomplete ListStatus = "incomplete"

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
// active record. SeasonCount / EpisodeCount are the trackable totals:
// aired episodes (present + missing) plus any pre-staged future
// episodes. Pure-pending slots (announced but not aired, no record
// yet) are EXCLUDED from both totals so the "X / Y" rollup tracks
// what the user can actually have on disk today. A season counts
// toward SeasonCount only if it has at least one trackable episode
// by the same rule. Renderers surface them as "available / total".
type ListRow struct {
	Status ListStatus `json:"status"`
	// IsAiring is the observed-airing flag, independent of Status. A
	// cour counts toward IsAiring when its first episode has aired (or
	// airs within the horizon) and its last episode is no older than the
	// configured airing tail. Split-cour gaps are non-airing until the
	// next cour nears start. Specials (season 0) are excluded. Computed
	// at row-build time; not persisted to series.json.
	IsAiring          bool          `json:"isAiring,omitempty"`
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
	Tags              []string      `json:"tags,omitempty"`
	// Series-level poster artwork URLs lifted from series.json. Empty
	// strings when the metadata provider had no poster for the series
	// or the row predates poster surfacing — clients should fall back
	// to a placeholder when both are empty.
	PosterURL          string `json:"posterUrl,omitempty"`
	PosterThumbnailURL string `json:"posterThumbnailUrl,omitempty"`
	DateAdded          string `json:"dateAdded,omitempty"`
	LastAired          string `json:"lastAired,omitempty"`
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
