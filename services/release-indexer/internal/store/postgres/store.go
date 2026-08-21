package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wyvernzora/kura/services/release-indexer/internal/cursor"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
)

const (
	defaultClaimLimit   = 1
	maxClaimLimit       = 500
	defaultLeaseSeconds = 300

	defaultReleasesLimit = 50
	maxReleasesLimit     = 500
	maxMagnetsBatch      = 500
)

const rawItemsByInfohashSQL = `
	SELECT id, source, source_id, title, NULLIF(url, ''), published_at, ingested_at
	FROM raw_items
	WHERE infohash = $1
	ORDER BY id ASC
`

type StoreConfig struct {
	QueueMaxAttempts int
}

type Store struct {
	pool  *pgxpool.Pool
	clock store.Clock
	cfg   StoreConfig
}

type releaseQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

var (
	_ store.Store = (*Store)(nil)
)

func NewStore(pool *pgxpool.Pool, clock store.Clock) *Store {
	return NewStoreWithConfig(pool, clock, StoreConfig{})
}

func NewStoreWithConfig(pool *pgxpool.Pool, clock store.Clock, cfg StoreConfig) *Store {
	if clock == nil {
		clock = store.RealClock{}
	}
	if cfg.QueueMaxAttempts <= 0 {
		cfg.QueueMaxAttempts = 3
	}
	return &Store{pool: pool, clock: clock, cfg: cfg}
}

func isCanonicalInfohash(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func ingestSeeds(p store.IngestParams) (magnet *string, sizeBytes *int64) {
	if p.Magnet != "" {
		m := p.Magnet
		magnet = &m
	}
	if p.SizeBytes != 0 {
		sz := p.SizeBytes
		sizeBytes = &sz
	}
	return magnet, sizeBytes
}

func (s *Store) IngestN(ctx context.Context, p store.IngestParams) (store.IngestOutcome, error) {
	if !isCanonicalInfohash(p.Infohash) {
		return store.IngestOutcome{Duplicate: true}, nil
	}

	now := s.clock.Now()
	magnet, sizeBytes := ingestSeeds(p)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.IngestOutcome{}, fmt.Errorf("ingest: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup after commit/error.

	created, inserted, err := s.upsertTx(ctx, tx, p, magnet, sizeBytes, now)
	if err != nil {
		return store.IngestOutcome{}, err
	}

	if created && !inserted {
		return store.IngestOutcome{Conflict: true}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return store.IngestOutcome{}, fmt.Errorf("ingest: commit: %w", err)
	}
	switch {
	case created:
		return store.IngestOutcome{New: true}, nil
	case inserted:
		return store.IngestOutcome{Updated: true}, nil
	default:
		return store.IngestOutcome{Duplicate: true}, nil
	}
}

func (s *Store) upsertTx(ctx context.Context, tx pgx.Tx, p store.IngestParams, magnet *string, sizeBytes *int64, now time.Time) (created, inserted bool, err error) {
	err = tx.QueryRow(ctx, `
		INSERT INTO releases (
			infohash, title, magnet, size_bytes, published_at, sources,
			match_status, attempt_count, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, ARRAY[$6]::text[],
			'unmatched', 0, $7, $7)
		ON CONFLICT (infohash) DO UPDATE SET infohash = releases.infohash
		RETURNING (xmax = 0) AS created
	`, p.Infohash, p.Title, magnet, sizeBytes, p.PublishedAt, p.Source, now).Scan(&created)
	if err != nil {
		return false, false, fmt.Errorf("ingest: upsert release: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO raw_items (
			infohash, source, source_id, title, url, published_at, ingested_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (source, source_id) DO NOTHING
	`, p.Infohash, p.Source, p.SourceID, p.Title, p.URL, p.PublishedAt, now)
	if err != nil {
		return false, false, fmt.Errorf("ingest: insert raw_item: %w", err)
	}
	inserted = tag.RowsAffected() > 0

	if !created && inserted {
		if err := s.recomputeRepresentative(ctx, tx, p.Infohash, magnet, sizeBytes, now); err != nil {
			return false, false, err
		}
	}
	return created, inserted, nil
}

func (s *Store) recomputeRepresentative(ctx context.Context, tx pgx.Tx, infohash string, magnet *string, sizeBytes *int64, now time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE releases r SET
			title = (
				SELECT ri.title FROM raw_items ri
				WHERE ri.infohash = r.infohash
				ORDER BY ri.published_at DESC, ri.id DESC
				LIMIT 1
			),
			published_at = (
				SELECT min(ri.published_at) FROM raw_items ri
				WHERE ri.infohash = r.infohash
			),
			sources = (
				SELECT array_agg(DISTINCT ri.source ORDER BY ri.source) FROM raw_items ri
				WHERE ri.infohash = r.infohash
			),
			magnet = COALESCE(r.magnet, $2),
			size_bytes = COALESCE(r.size_bytes, $3),
			updated_at = $4
		WHERE r.infohash = $1
	`, infohash, magnet, sizeBytes, now)
	if err != nil {
		return fmt.Errorf("ingest: recompute representative fields: %w", err)
	}
	return nil
}

func (s *Store) Claim(ctx context.Context, p store.ClaimParams) (store.ClaimResult, error) {
	now := s.clock.Now()
	limit := p.Limit
	if limit <= 0 {
		limit = defaultClaimLimit
	}
	if limit > maxClaimLimit {
		limit = maxClaimLimit
	}
	leaseSeconds := p.LeaseSeconds
	if leaseSeconds <= 0 {
		leaseSeconds = defaultLeaseSeconds
	}
	leaseExpires := now.Add(time.Duration(leaseSeconds) * time.Second)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.ClaimResult{}, fmt.Errorf("claim: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup after commit/error.

	if _, err := tx.Exec(ctx, `
		UPDATE releases
		SET match_status = 'exhausted',
			claimed_at = NULL,
			lease_expires_at = NULL,
			updated_at = $1
		WHERE match_status = 'unmatched'
			AND attempt_count >= $2
			AND (lease_expires_at IS NULL OR lease_expires_at <= $1)
	`, now, s.cfg.QueueMaxAttempts); err != nil {
		return store.ClaimResult{}, fmt.Errorf("claim: exhaust old attempts: %w", err)
	}

	rows, err := tx.Query(ctx, `
		WITH claimable AS (
			SELECT infohash
			FROM releases
			WHERE match_status = 'unmatched'
				AND attempt_count < $4
				AND (lease_expires_at IS NULL OR lease_expires_at <= $1)
			ORDER BY published_at DESC, infohash DESC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		),
		leased AS (
			UPDATE releases r SET
				claimed_at = $1,
				lease_expires_at = $3,
				claim_token = r.claim_token + 1,
				updated_at = $1
			FROM claimable c
			WHERE r.infohash = c.infohash
			RETURNING r.infohash, r.claim_token, r.attempt_count, r.lease_expires_at, r.published_at
		)
		SELECT infohash, claim_token, attempt_count, lease_expires_at
		FROM leased
		ORDER BY published_at DESC, infohash DESC
	`, now, limit, leaseExpires, s.cfg.QueueMaxAttempts)
	if err != nil {
		return store.ClaimResult{}, fmt.Errorf("claim: select+lease: %w", err)
	}

	type claimedRow struct {
		infohash     string
		token        int64
		attemptCount int
		leaseExpires time.Time
	}
	claimed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (claimedRow, error) {
		var cr claimedRow
		err := row.Scan(&cr.infohash, &cr.token, &cr.attemptCount, &cr.leaseExpires)
		return cr, err
	})
	if err != nil {
		return store.ClaimResult{}, fmt.Errorf("claim: collect leased rows: %w", err)
	}

	items := make([]store.ClaimedRelease, 0, len(claimed))
	for _, cr := range claimed {
		raw, err := rawItemsFor(ctx, tx, cr.infohash, "claim")
		if err != nil {
			return store.ClaimResult{}, err
		}
		items = append(items, store.ClaimedRelease{
			Infohash:     cr.infohash,
			ClaimToken:   cr.token,
			AttemptCount: cr.attemptCount,
			LeaseExpires: cr.leaseExpires,
			RawItems:     raw,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return store.ClaimResult{}, fmt.Errorf("claim: commit: %w", err)
	}
	return store.ClaimResult{Items: items}, nil
}

func rawItemsFor(ctx context.Context, q releaseQuerier, infohash, op string) ([]store.RawItemDetail, error) {
	rows, err := q.Query(ctx, rawItemsByInfohashSQL, infohash)
	if err != nil {
		return nil, fmt.Errorf("%s: load raw_items for %s: %w", op, infohash, err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.RawItemDetail, error) {
		var item store.RawItemDetail
		err := row.Scan(&item.ID, &item.Source, &item.SourceID, &item.Title, &item.URL, &item.PublishedAt, &item.IngestedAt)
		return item, err
	})
	if err != nil {
		return nil, fmt.Errorf("%s: collect raw_items for %s: %w", op, infohash, err)
	}
	return items, nil
}

func (s *Store) Submit(ctx context.Context, p store.SubmitParams) error { //nolint:cyclop // Small status transition table; splitting hides the flow.
	if err := validateSubmit(p); err != nil {
		return err
	}
	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("submit: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup after commit/error.

	var currentStatus string
	var currentToken int64
	var leaseExpires *time.Time
	var attemptCount int
	err = tx.QueryRow(ctx, `
		SELECT match_status, claim_token, lease_expires_at, attempt_count
		FROM releases
		WHERE infohash = $1
		FOR UPDATE
	`, p.Infohash).Scan(&currentStatus, &currentToken, &leaseExpires, &attemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("submit %s: %w", p.Infohash, store.ErrNoSuchRelease)
	}
	if err != nil {
		return fmt.Errorf("submit %s: load lease: %w", p.Infohash, err)
	}
	if currentToken != p.ClaimToken || leaseExpires == nil || !leaseExpires.After(now) {
		return fmt.Errorf("submit %s: %w", p.Infohash, store.ErrStaleLease)
	}
	if currentStatus != "unmatched" {
		return fmt.Errorf("submit %s: %w", p.Infohash, store.ErrNoActiveLease)
	}

	newAttemptCount := attemptCount
	if p.Status == "unmatched" {
		newAttemptCount++
	}
	finalStatus := p.Status
	clearLease := p.Status == "matched" || p.Status == "suppressed"
	if p.Status == "unmatched" && newAttemptCount >= s.cfg.QueueMaxAttempts {
		finalStatus = "exhausted"
		clearLease = true
	}

	switch finalStatus {
	case "matched":
		_, err = tx.Exec(ctx, `
			UPDATE releases SET
				match_status = 'matched',
				ref = $2,
				confidence = $3,
				first_matched_at = COALESCE(first_matched_at, $4),
				claimed_at = NULL,
				lease_expires_at = NULL,
				updated_at = $4
			WHERE infohash = $1
		`, p.Infohash, p.Ref, nullableConfidence(finalStatus, p.Confidence), now)
	case "suppressed", "exhausted":
		_, err = tx.Exec(ctx, `
			UPDATE releases SET
				match_status = $2,
				attempt_count = $3,
				ref = NULL,
				confidence = NULL,
				claimed_at = NULL,
				lease_expires_at = NULL,
				updated_at = $4
			WHERE infohash = $1
		`, p.Infohash, finalStatus, newAttemptCount, now)
	case "unmatched":
		_, err = tx.Exec(ctx, `
			UPDATE releases SET
				attempt_count = $2,
				updated_at = $3
			WHERE infohash = $1
		`, p.Infohash, newAttemptCount, now)
	default:
		return fmt.Errorf("submit: invalid status %q", p.Status)
	}
	if err != nil {
		return fmt.Errorf("submit %s: update: %w", p.Infohash, err)
	}
	if !clearLease && finalStatus != "unmatched" {
		return fmt.Errorf("submit: impossible status %q", finalStatus)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO match_events (infohash, status, ref, confidence, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, p.Infohash, finalStatus, nullableRef(finalStatus, p.Ref), nullableConfidence(finalStatus, p.Confidence), p.Reason, now); err != nil {
		return fmt.Errorf("submit %s: append match_event: %w", p.Infohash, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("submit %s: commit: %w", p.Infohash, err)
	}
	return nil
}

// statusTransitions IS the state-machine validation for SetStatus: a
// transition exists here or it is rejected. Two boundaries keep this from
// becoming queue control. Transitions the matcher's claim-fenced submit path
// owns (unmatched -> matched/suppressed) are deliberately absent, so submit
// stays the only route to them; and no source state here carries a claim lease,
// since leases exist only on unmatched rows.
//
// The operator rows are what the release-attention surface acts through: a hand
// match out of suppressed or exhausted, an affirm/correct of an existing
// low-confidence match, a requeue of an exhausted release back into the claim
// queue, and a discard of an exhausted release into suppressed. Suppression
// out of unmatched stays submit-only — an operator discards what the matcher
// already gave up on, not what is still in the queue.
var statusTransitions = map[string]map[string]bool{
	"matched":    {"dead": true, "matched": true},
	"suppressed": {"matched": true},
	"exhausted":  {"matched": true, "unmatched": true, "suppressed": true},
}

// operatorConfidence is the score an operator's own match records. A person
// deciding by hand is certain by definition; this is not a matching threshold —
// the indexer holds none.
const operatorConfidence = 1.0

func (s *Store) SetStatus(ctx context.Context, p store.SetStatusParams) error {
	// A ref on a target that takes none is a malformed body whatever the row
	// says, so it is refused before a transaction is opened for it.
	if p.Status != "matched" && p.Ref != "" {
		return fmt.Errorf("set_status %s: %s takes no ref: %w",
			p.Infohash, p.Status, store.ErrRefForbidden)
	}

	now := s.clock.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("set_status: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup after commit/error.

	var currentStatus string
	var currentRef *string
	var currentConfidence *float64
	err = tx.QueryRow(ctx, `
		SELECT match_status, ref, confidence
		FROM releases
		WHERE infohash = $1
		FOR UPDATE
	`, p.Infohash).Scan(&currentStatus, &currentRef, &currentConfidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("set_status %s: %w", p.Infohash, store.ErrNoSuchRelease)
	}
	if err != nil {
		return fmt.Errorf("set_status %s: load status: %w", p.Infohash, err)
	}

	// Idempotent no-op: natural PUT semantics, and it keeps a retried call
	// from appending a second audit row for a transition already made.
	if isSetStatusNoOp(p, currentStatus, currentRef, currentConfidence) {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("set_status %s: commit: %w", p.Infohash, err)
		}
		return nil
	}

	// The table is checked before the ref a `matched` target requires, so a
	// transition that was never on offer is reported as the conflict it is
	// rather than as a complaint about the body.
	if !statusTransitions[currentStatus][p.Status] {
		return fmt.Errorf("set_status %s: %s -> %s: %w",
			p.Infohash, currentStatus, p.Status, store.ErrInvalidTransition)
	}
	if p.Status == "matched" {
		if p.Ref == "" {
			return fmt.Errorf("set_status %s: matched needs a ref: %w", p.Infohash, store.ErrRefRequired)
		}
		if err := cursor.ValidateRef(p.Ref); err != nil {
			return fmt.Errorf("set_status %s: %w", p.Infohash, err)
		}
	}

	if err := s.applySetStatus(ctx, tx, p, now); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO match_events (infohash, status, ref, confidence, reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, p.Infohash, p.Status, nullableRef(p.Status, p.Ref),
		setStatusEventConfidence(p.Status), nullableReason(p.Reason), now); err != nil {
		return fmt.Errorf("set_status %s: append match_event: %w", p.Infohash, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("set_status %s: commit: %w", p.Infohash, err)
	}
	return nil
}

// applySetStatus writes the row change for one allowed transition. Each target
// owns what it touches: a hand match records the ref an operator stands behind,
// a requeue resets the exhaustion accounting that took the release out of the
// queue, and dead changes only the status.
func (s *Store) applySetStatus(ctx context.Context, tx pgx.Tx, p store.SetStatusParams, now time.Time) error {
	var err error
	switch p.Status {
	case "matched":
		_, err = tx.Exec(ctx, `
			UPDATE releases SET
				match_status = 'matched',
				ref = $2,
				confidence = $3,
				first_matched_at = COALESCE(first_matched_at, $4),
				claimed_at = NULL,
				lease_expires_at = NULL,
				updated_at = $4
			WHERE infohash = $1
		`, p.Infohash, p.Ref, operatorConfidence, now)
	case "unmatched":
		// Requeue. attempt_count and the lease columns are what kept the row
		// out of the claim queue, so clearing them IS the requeue; leaving
		// attempt_count in place would exhaust the release again on the next
		// claim sweep without a single new attempt.
		_, err = tx.Exec(ctx, `
			UPDATE releases SET
				match_status = 'unmatched',
				attempt_count = 0,
				claimed_at = NULL,
				lease_expires_at = NULL,
				updated_at = $2
			WHERE infohash = $1
		`, p.Infohash, now)
	default:
		// dead or suppressed. ref, confidence, first_matched_at and
		// attempt_count are deliberately preserved: the match history is why
		// a dead release is not re-selected, and a suppressed release keeps
		// the accounting that led the operator to discard it.
		_, err = tx.Exec(ctx, `
			UPDATE releases SET
				match_status = $2,
				updated_at = $3
			WHERE infohash = $1
		`, p.Infohash, p.Status, now)
	}
	if err != nil {
		return fmt.Errorf("set_status %s: update: %w", p.Infohash, err)
	}
	return nil
}

// isSetStatusNoOp reports whether applying the request would change nothing,
// which a retried PUT must not re-audit. For a hand match that means the row
// already carries this ref AT operator confidence: an already-matched release
// still holding the matcher's own lower score is an affirm to be applied, not a
// repeat to be skipped, and a different ref is a correction.
func isSetStatusNoOp(p store.SetStatusParams, currentStatus string, currentRef *string, currentConfidence *float64) bool {
	if currentStatus != p.Status {
		return false
	}
	if p.Status != "matched" {
		return true
	}
	return currentRef != nil && *currentRef == p.Ref &&
		currentConfidence != nil && *currentConfidence == operatorConfidence
}

// setStatusEventConfidence scores the audit row. Only an operator match carries
// a score; dead and requeue record none.
func setStatusEventConfidence(status string) any {
	if status == "matched" {
		return operatorConfidence
	}
	return nil
}

func nullableReason(reason string) any {
	if reason == "" {
		return nil
	}
	return reason
}

func validateSubmit(p store.SubmitParams) error {
	switch p.Status {
	case "matched":
		return cursor.ValidateRef(p.Ref)
	case "unmatched", "suppressed":
		return nil
	default:
		return fmt.Errorf("submit: invalid status %q", p.Status)
	}
}

func nullableRef(status, ref string) any {
	if status == "matched" {
		return ref
	}
	return nil
}

func nullableConfidence(status string, confidence *float64) any {
	if (status == "matched" || status == "suppressed") && confidence != nil {
		return *confidence
	}
	return nil
}

func (s *Store) QueueStats(ctx context.Context) (store.QueueStats, error) {
	now := s.clock.Now()
	var qs store.QueueStats
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (
				WHERE match_status = 'unmatched'
					AND attempt_count < $2
					AND (lease_expires_at IS NULL OR lease_expires_at <= $1)
			),
			count(*) FILTER (
				WHERE match_status = 'unmatched'
					AND lease_expires_at IS NOT NULL
					AND lease_expires_at > $1
			),
			count(*) FILTER (WHERE match_status = 'unmatched'),
			count(*) FILTER (WHERE match_status = 'matched'),
			count(*) FILTER (WHERE match_status = 'suppressed'),
			count(*) FILTER (WHERE match_status = 'exhausted'),
			count(*) FILTER (WHERE match_status = 'dead')
		FROM releases
	`, now, s.cfg.QueueMaxAttempts).Scan(&qs.Available, &qs.Leased, &qs.Unmatched, &qs.Matched, &qs.Suppressed, &qs.Exhausted, &qs.Dead)
	if err != nil {
		return store.QueueStats{}, fmt.Errorf("queue stats: %w", err)
	}
	return qs, nil
}

func (s *Store) CatalogStats(ctx context.Context) (store.CatalogStats, error) {
	var cs store.CatalogStats
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM raw_items),
			(SELECT count(*) FROM releases),
			(SELECT count(DISTINCT ref) FROM releases WHERE ref IS NOT NULL AND ref <> '')
	`).Scan(&cs.RawPosts, &cs.Infohashes, &cs.Refs)
	if err != nil {
		return store.CatalogStats{}, fmt.Errorf("catalog stats: %w", err)
	}
	return cs, nil
}

func (s *Store) ListReleases(ctx context.Context, q store.ReleaseQuery) (store.ReleasePage, error) {
	if q.Ref != "" {
		if err := cursor.ValidateRef(q.Ref); err != nil {
			return store.ReleasePage{}, err
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultReleasesLimit
	}
	if limit > maxReleasesLimit {
		limit = maxReleasesLimit
	}

	path := cursor.PathCatalog
	if q.Since != nil {
		path = cursor.PathDelta
	}
	binding := cursor.Binding{
		Ref:           q.Ref,
		Path:          path,
		Statuses:      normalizeStatuses(q.Statuses),
		MaxConfidence: q.MaxConfidence,
	}
	var seek *cursor.Cursor
	if q.Cursor != "" {
		c, err := cursor.Decode(q.Cursor, binding)
		if err != nil {
			return store.ReleasePage{}, cursor.ErrInvalidCursor
		}
		seek = &c
	}
	fetch := limit + 1
	seekKey, seekHash, hasSeek := time.Time{}, "", false
	if seek != nil {
		seekKey, seekHash, hasSeek = seek.Key, seek.Infohash, true
	}

	rows, err := s.listReleaseRows(ctx, q, binding, seekKey, seekHash, hasSeek, fetch)
	if err != nil {
		return store.ReleasePage{}, fmt.Errorf("list_releases %s: %w", q.Ref, err)
	}

	type keyedRow struct {
		item store.ReleaseItem
		key  time.Time
	}
	keyed, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (keyedRow, error) {
		var kr keyedRow
		err := row.Scan(&kr.item.Infohash, &kr.item.Ref, &kr.item.Title, &kr.item.SizeBytes,
			&kr.item.PublishedAt, &kr.item.Confidence, &kr.item.Sources, &kr.item.MatchStatus, &kr.key)
		return kr, err
	})
	if err != nil {
		return store.ReleasePage{}, fmt.Errorf("list_releases %s: collect: %w", q.Ref, err)
	}

	hasMore := len(keyed) > limit
	if hasMore {
		keyed = keyed[:limit]
	}
	items := make([]store.ReleaseItem, len(keyed))
	for i := range keyed {
		items[i] = keyed[i].item
	}
	var next string
	if hasMore {
		last := keyed[len(keyed)-1]
		enc, err := cursor.Encode(cursor.Cursor{
			Binding: binding, Key: last.key, Infohash: last.item.Infohash,
		})
		if err != nil {
			return store.ReleasePage{}, fmt.Errorf("list_releases %s: encode cursor: %w", q.Ref, err)
		}
		next = enc
	}
	return store.ReleasePage{Releases: items, NextCursor: next}, nil
}

// normalizeStatuses sorts and deduplicates a status filter so two spellings of
// the same filter produce the same cursor binding. An empty filter stays empty:
// that is the matched-only default, not a filter over nothing.
func normalizeStatuses(statuses []string) []string {
	if len(statuses) == 0 {
		return nil
	}
	out := slices.Clone(statuses)
	slices.Sort(out)
	return slices.Compact(out)
}

// listReleaseRows builds the one page query. The predicates are optional and
// independent, so the SQL is assembled rather than enumerated — sixteen literal
// variants of the same statement is not a readability win.
//
// The unfiltered default keeps the literal `match_status = 'matched'` the
// partial indexes are built on; a status filter is the operator surface's path
// and pays an ordinary scan.
func (s *Store) listReleaseRows(ctx context.Context, q store.ReleaseQuery, binding cursor.Binding, seekKey time.Time, seekHash string, hasSeek bool, fetch int) (pgx.Rows, error) {
	keyCol := "published_at"
	if binding.Path == cursor.PathDelta {
		keyCol = "first_matched_at"
	}

	args := make([]any, 0, 8)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	var b strings.Builder
	fmt.Fprintf(&b, `
		SELECT infohash, COALESCE(ref, ''), title, size_bytes, published_at,
			confidence, sources, match_status, %s
		FROM releases
	`, keyCol)
	if len(binding.Statuses) == 0 {
		b.WriteString("\t\tWHERE match_status = 'matched'\n")
	} else {
		fmt.Fprintf(&b, "\t\tWHERE match_status = ANY(%s::match_status[])\n", arg(binding.Statuses))
	}
	if q.Ref != "" {
		fmt.Fprintf(&b, "\t\t\tAND ref = %s\n", arg(q.Ref))
	}
	if q.Since != nil {
		fmt.Fprintf(&b, "\t\t\tAND first_matched_at > %s\n", arg(*q.Since))
	}
	if binding.MaxConfidence != nil {
		// Strictly less than, so an unscored (NULL) match is excluded: a row
		// with no score is not a low-confidence row.
		fmt.Fprintf(&b, "\t\t\tAND confidence < %s\n", arg(*binding.MaxConfidence))
	}
	fmt.Fprintf(&b, "\t\t\tAND (NOT %s OR (%s, infohash) < (%s, %s))\n",
		arg(hasSeek), keyCol, arg(seekKey), arg(seekHash))
	fmt.Fprintf(&b, "\t\tORDER BY %s DESC, infohash DESC\n\t\tLIMIT %s\n", keyCol, arg(fetch))

	return s.pool.Query(ctx, b.String(), args...)
}

func (s *Store) GetRelease(ctx context.Context, infohash string) (store.ReleaseDetail, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return store.ReleaseDetail{}, fmt.Errorf("get_release %s: begin tx: %w", infohash, err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort cleanup after commit/error.

	var out store.ReleaseDetail
	err = tx.QueryRow(ctx, `
		SELECT infohash, title, magnet, size_bytes, published_at, sources,
			match_status, ref, confidence, first_matched_at, attempt_count,
			created_at, updated_at
		FROM releases
		WHERE infohash = $1
	`, infohash).Scan(
		&out.Infohash, &out.Title, &out.Magnet, &out.SizeBytes, &out.PublishedAt, &out.Sources,
		&out.MatchStatus, &out.Ref, &out.Confidence, &out.FirstMatchedAt, &out.AttemptCount,
		&out.CreatedAt, &out.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ReleaseDetail{}, fmt.Errorf("get_release %s: %w", infohash, store.ErrNoSuchRelease)
	}
	if err != nil {
		return store.ReleaseDetail{}, fmt.Errorf("get_release %s: %w", infohash, err)
	}

	rawItems, err := rawItemsFor(ctx, tx, infohash, "get_release "+infohash)
	if err != nil {
		return store.ReleaseDetail{}, err
	}
	matchEvents, err := s.releaseMatchEvents(ctx, tx, infohash)
	if err != nil {
		return store.ReleaseDetail{}, err
	}
	out.RawItems = rawItems
	out.MatchEvents = matchEvents
	if err := tx.Commit(ctx); err != nil {
		return store.ReleaseDetail{}, fmt.Errorf("get_release %s: commit: %w", infohash, err)
	}
	return out, nil
}

func (s *Store) releaseMatchEvents(ctx context.Context, q releaseQuerier, infohash string) ([]store.MatchEventDetail, error) {
	rows, err := q.Query(ctx, `
		SELECT id, status, ref, confidence, NULLIF(reason, ''), created_at
		FROM match_events
		WHERE infohash = $1
		ORDER BY created_at ASC, id ASC
	`, infohash)
	if err != nil {
		return nil, fmt.Errorf("get_release %s: load match_events: %w", infohash, err)
	}
	events, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.MatchEventDetail, error) {
		var ev store.MatchEventDetail
		err := row.Scan(&ev.ID, &ev.Status, &ev.Ref, &ev.Confidence, &ev.Reason, &ev.CreatedAt)
		return ev, err
	})
	if err != nil {
		return nil, fmt.Errorf("get_release %s: collect match_events: %w", infohash, err)
	}
	return events, nil
}

func (s *Store) ResolveMagnets(ctx context.Context, infohashes []string) (map[string]string, error) {
	if len(infohashes) > maxMagnetsBatch {
		return nil, fmt.Errorf("resolve_magnets: batch of %d exceeds hard cap %d", len(infohashes), maxMagnetsBatch)
	}
	out := make(map[string]string, len(infohashes))
	if len(infohashes) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT infohash, magnet
		FROM releases
		WHERE infohash = ANY($1) AND magnet IS NOT NULL AND match_status = 'matched'
	`, infohashes)
	if err != nil {
		return nil, fmt.Errorf("resolve_magnets: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ih, magnet string
		if err := rows.Scan(&ih, &magnet); err != nil {
			return nil, fmt.Errorf("resolve_magnets: scan: %w", err)
		}
		out[ih] = magnet
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("resolve_magnets: rows: %w", err)
	}
	return out, nil
}

func (s *Store) Ping(ctx context.Context) error {
	if s.pool == nil {
		return errors.New("indexer/postgres: nil pool")
	}
	return s.pool.Ping(ctx)
}

func (s *Store) Close() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}
