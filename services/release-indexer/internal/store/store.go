package store

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNoSuchRelease = errors.New("indexer: no such release")
	ErrNoActiveLease = errors.New("indexer: no active lease")
	ErrStaleLease    = errors.New("indexer: stale lease")
	// ErrInvalidTransition reports a status change the transition table
	// does not allow. The wrapping error names the attempted transition.
	ErrInvalidTransition = errors.New("indexer: invalid status transition")
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type Store interface {
	Ping(ctx context.Context) error
	IngestN(ctx context.Context, p IngestParams) (IngestOutcome, error)
	Claim(ctx context.Context, p ClaimParams) (ClaimResult, error)
	Submit(ctx context.Context, p SubmitParams) error
	SetStatus(ctx context.Context, p SetStatusParams) error
	QueueStats(ctx context.Context) (QueueStats, error)
	CatalogStats(ctx context.Context) (CatalogStats, error)
	ListReleases(ctx context.Context, q ReleaseQuery) (ReleasePage, error)
	GetRelease(ctx context.Context, infohash string) (ReleaseDetail, error)
	ResolveMagnets(ctx context.Context, infohashes []string) (map[string]string, error)
	Close() error
}

type IngestParams struct {
	Infohash    string
	Source      string
	SourceID    string
	Title       string
	URL         string
	Magnet      string
	SizeBytes   int64
	PublishedAt time.Time
}

type IngestOutcome struct {
	New       bool
	Updated   bool
	Duplicate bool
	Conflict  bool
}

type ClaimParams struct {
	Limit        int
	LeaseSeconds int
}

type ClaimResult struct {
	Items []ClaimedRelease
}

type ClaimedRelease struct {
	Infohash     string
	ClaimToken   int64
	AttemptCount int
	LeaseExpires time.Time
	RawItems     []RawItemDetail
}

type SubmitParams struct {
	Infohash   string
	ClaimToken int64
	Status     string
	Ref        string
	Confidence *float64
	Reason     string
}

// SetStatusParams is one operator-driven status change. It is deliberately
// not claim-fenced: the transition table excludes every transition the
// matcher's claim path owns, so the two routes cannot collide.
type SetStatusParams struct {
	Infohash string
	Status   string
	Reason   string
}

type QueueStats struct {
	Available  int
	Leased     int
	Unmatched  int
	Matched    int
	Suppressed int
	Exhausted  int
	Dead       int
}

type CatalogStats struct {
	RawPosts   int
	Infohashes int
	Refs       int
}

type ReleaseQuery struct {
	Ref    string
	Since  *time.Time
	Limit  int
	Cursor string
}

type ReleasePage struct {
	Releases   []ReleaseItem
	NextCursor string
}

type ReleaseItem struct {
	Infohash string
	Ref      string
	Title    string
	// SizeBytes and Confidence are nullable columns and stay pointers all
	// the way out: a release with no recorded size is not a release of
	// size zero, and an unscored match is not a match scored 0.0.
	SizeBytes   *int64
	PublishedAt time.Time
	Confidence  *float64
	Sources     []string
}

type ReleaseDetail struct {
	Infohash       string
	Title          string
	Magnet         *string
	SizeBytes      *int64
	PublishedAt    time.Time
	Sources        []string
	MatchStatus    string
	Ref            *string
	Confidence     *float64
	FirstMatchedAt *time.Time
	AttemptCount   int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	RawItems       []RawItemDetail
	MatchEvents    []MatchEventDetail
}

type RawItemDetail struct {
	ID          int64
	Source      string
	SourceID    string
	Title       string
	URL         *string
	PublishedAt time.Time
	IngestedAt  time.Time
}

type MatchEventDetail struct {
	ID         int64
	Status     string
	Ref        *string
	Confidence *float64
	Reason     *string
	CreatedAt  time.Time
}
