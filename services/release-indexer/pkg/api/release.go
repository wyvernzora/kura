package api

import "time"

// MatchStatus is the closed set of release match states.
type MatchStatus string

const (
	MatchStatusUnmatched  MatchStatus = "unmatched"
	MatchStatusMatched    MatchStatus = "matched"
	MatchStatusSuppressed MatchStatus = "suppressed"
	MatchStatusExhausted  MatchStatus = "exhausted"
	// MatchStatusDead marks a release whose torrent proved undownloadable.
	// It is a terminal curation state, not a queue state: the matcher never
	// submits it, list/claim paths already exclude it by status, and there
	// is no transition back in v1.
	MatchStatusDead MatchStatus = "dead"
)

// SetStatusRequest is the PUT /api/releases/v1/{infohash}/status body.
//
// Reason is optional and is recorded to match_events, the existing audit
// trail — the transition needs no new columns.
//
// Ref carries the operator's hand match. It is required for a transition into
// `matched` and rejected for every other target, which take no ref.
type SetStatusRequest struct {
	Status MatchStatus `json:"status"`
	Ref    string      `json:"ref,omitempty"`
	Reason string      `json:"reason,omitempty"`
}

// ReleaseItem is one row of GET /api/releases/v1.
//
// SizeBytes and Confidence are pointers because both columns are nullable and
// the distinction matters: a release with no recorded size is not a release of
// size zero, and an unscored match is not a match scored 0.0. The pre-migration
// list query coerced both with COALESCE(..., 0) and lost that.
//
// MatchStatus is always populated: the list is no longer matched-only, so a row
// has to say which state it is in.
type ReleaseItem struct {
	Infohash    string      `json:"infohash"`
	Ref         string      `json:"ref"`
	Title       string      `json:"title"`
	SizeBytes   *int64      `json:"sizeBytes"`
	PublishedAt time.Time   `json:"publishedAt"`
	Confidence  *float64    `json:"confidence"`
	Sources     []string    `json:"sources"`
	MatchStatus MatchStatus `json:"matchStatus"`
}

// ReleaseList is the GET /api/releases/v1 response. Items is never null.
// NextCursor is omitted when there is no next page.
type ReleaseList struct {
	Items      []ReleaseItem `json:"items"`
	NextCursor *string       `json:"nextCursor,omitempty"`
}

// ReleaseDetail is the GET /api/releases/v1/{infohash} response.
//
// Every nullable database fact stays an explicit JSON null rather than being
// omitted, so a consumer can tell "not recorded" from "not returned".
type ReleaseDetail struct {
	Infohash       string       `json:"infohash"`
	Title          string       `json:"title"`
	Magnet         *string      `json:"magnet"`
	SizeBytes      *int64       `json:"sizeBytes"`
	PublishedAt    time.Time    `json:"publishedAt"`
	Sources        []string     `json:"sources"`
	MatchStatus    MatchStatus  `json:"matchStatus"`
	Ref            *string      `json:"ref"`
	Confidence     *float64     `json:"confidence"`
	FirstMatchedAt *time.Time   `json:"firstMatchedAt"`
	AttemptCount   int          `json:"attemptCount"`
	CreatedAt      time.Time    `json:"createdAt"`
	UpdatedAt      time.Time    `json:"updatedAt"`
	RawItems       []RawItem    `json:"rawItems"`
	MatchEvents    []MatchEvent `json:"matchEvents"`
}

// RawItem is one source posting linked to a release, ordered by id ascending.
type RawItem struct {
	ID          int64     `json:"id"`
	Source      string    `json:"source"`
	SourceID    string    `json:"sourceId"`
	Title       string    `json:"title"`
	URL         *string   `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	IngestedAt  time.Time `json:"ingestedAt"`
}

// MatchEvent is one entry of a release's match history, ordered by createdAt
// then id ascending.
type MatchEvent struct {
	ID         int64       `json:"id"`
	Status     MatchStatus `json:"status"`
	Ref        *string     `json:"ref"`
	Confidence *float64    `json:"confidence"`
	Reason     *string     `json:"reason"`
	CreatedAt  time.Time   `json:"createdAt"`
}

// Magnet is the GET /api/releases/v1/{infohash}/magnet response.
//
// There is no bulk form. A caller needing several magnets issues several
// requests; a batch endpoint would need a cap, a partial-failure shape, and a
// response-size budget to save a handful of round trips.
type Magnet struct {
	Infohash string `json:"infohash"`
	Magnet   string `json:"magnet"`
}
