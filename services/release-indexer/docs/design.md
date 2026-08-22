# Release indexer — Design

The release indexer is a durable anime release index: it crawls configured sources,
stores raw release posts, leases
unmatched releases to an external matcher, records the matcher outcome, and lets a
consumer agent list matched releases with optional canonical-ref filtering.

## Invariants

- The release identity is the canonical v1 `btih`: 40 lowercase hex. Pure v2 torrents
  are skipped.
- The indexer does not match titles. It records the external matcher result.
- `ref` values are opaque namespace-prefixed strings such as `tvdb:123`; the indexer only
  shape-validates them.
- DMHY and Nyaa run inside the indexer process. Each source has one non-overlapping
  scheduled loop that starts at the newest listing and ingests, page by page,
  everything newer than its configured settle window.
- Normal crawling has no durable cursor, bootstrap window, or overlap state; the
  settle window is the loop's only time parameter. Listing state older than the
  settle window is assumed immutable, and idempotent ingestion makes repeated
  reads safe.
- `POST /api/releases/v1/ingest` remains the external-producer surface; sources still emit the
  same `RawPost` contract and do not import indexer storage. The sanctioned backfill
  path is `POST /api/releases/v1/sources/{source}/crawl`: a stateless count-and-cursor
  chunk that uses the scheduled loop's crawler instance and ingests directly.
  The client owns the cursor; the indexer persists no crawl position.
- Queue claims are fenced by `claim_token`; stale submits must not overwrite newer
  claims.
- The indexer stores the full crawler-provided magnet link. It does not normalize,
  refresh, probe, or reassemble tracker URLs.

## Data Model

The schema has three tables:

- `releases`: one row per infohash. Holds representative title, full magnet,
  `size_bytes`, first-seen `published_at`, source set, match status, `ref`,
  confidence, claim bookkeeping, and timestamps.
- `raw_items`: append-only parsed crawler posts keyed by `(source, source_id)`.
- `match_events`: minimal append-only submit log with `status`, `ref`, `confidence`,
  `reason`, and `created_at`.

Release statuses are deliberately small:

- `unmatched`: not matched yet and claimable when not leased and under the failed-attempt cap.
- `matched`: matched to a canonical ref the user cares about.
- `suppressed`: not wanted, matched or not.
- `exhausted`: too many failed attempts; no longer offered as work until an operator
  hand matches it or requeues it.
- `dead`: matched, but the torrent proved undownloadable. Terminal curation state set by
  the operator, never by the matcher; there is no transition back in v1.

No `defer`, `escalate`, `reopen`, `next_eligible_at`, recheck state, provenance fields,
or matcher attributes exist in this pass.

## REST API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/releases/v1` | List releases, newest first; matched-only by default, optionally narrowed by ref, match status, or confidence ceiling. |
| `POST` | `/api/releases/v1/ingest` | Accept a batch of crawler posts. |
| `POST` | `/api/releases/v1/sources/{source}/crawl` | Consume one count-and-cursor source chunk and ingest it directly (operator backfill). |
| `GET` | `/api/releases/v1/{infohash}/magnet` | Get the stored magnet URI for one release, only while it is `matched`. |
| `PUT` | `/api/releases/v1/{infohash}/status` | Operator status change, fenced by the transition table below. |
| `GET` | `/api/releases/v1/{infohash}` | Get one release detail, raw source evidence, and match history. |
| `POST` | `/api/releases/v1/queue/claim` | Lease claimable unmatched releases. |
| `GET` | `/api/releases/v1/queue/stats` | Return queue/status counts, including exhausted and dead. |
| `POST` | `/api/releases/v1/queue/submit` | Submit `matched`, `unmatched`, or `suppressed` for a claim. |
| `GET` | `/healthz` | DB ping; returns `{ok, version}`. |
| `GET` | `/metrics` | Prometheus metrics. Served on `server.metrics_addr`, not the API listener. |

Crawler posts and ingest posts use the same shape:

```json
{
  "source": "dmhy",
  "sourceId": "721238",
  "title": "raw release title",
  "magnet": "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
  "url": "https://share.dmhy.org/topics/view/721238_example.html",
  "publishedAt": "2026-06-24T12:00:00Z",
  "sizeBytes": 3600000000
}
```

`/api/releases/v1/queue/claim` returns `claimToken`, `attemptCount`, `leaseExpiresAt`, linked
`rawItems`, and `precedents`:

```json
{
  "infohash": "0123456789abcdef0123456789abcdef01234567",
  "claimToken": 12,
  "attemptCount": 0,
  "leaseExpiresAt": "2026-06-24T12:05:00Z",
  "rawItems": [
    {
      "id": 42,
      "source": "dmhy",
      "sourceId": "721238",
      "title": "raw release title",
      "url": "https://share.dmhy.org/topics/view/721238_example.html",
      "publishedAt": "2026-06-24T12:00:00Z"
    }
  ],
  "precedents": [
    {
      "infohash": "89abcdef0123456789abcdef0123456789abcdef",
      "title": "[Group] Example - 00 (WebRip 1080p)",
      "matchStatus": "matched",
      "ref": "tvdb:123",
      "reason": null,
      "similarity": 0.87,
      "publishedAt": "2026-06-17T12:00:00Z"
    }
  ]
}
```

`precedents` is the three `matched` or `suppressed` releases nearest the claimed title
by pg_trgm trigram similarity, each with its score and, for a suppression, the reason
recorded with it. The indexer applies no cutoff: which score is close enough to carry a
prior decision forward is matcher policy, and a threshold here would put a matching
heuristic in the store. Trigram similarity carries decisions forward within a naming
lineage — the same group's next episode, another re-encode of a title already
suppressed — and does not bridge translations, so a romaji and an English title for the
same series do not precede each other. The array is never null: a catalog with nothing
decided yields `[]`.

`/api/releases/v1/queue/submit` accepts:

```json
{
  "infohash": "0123456789abcdef0123456789abcdef01234567",
  "claimToken": 12,
  "status": "matched",
  "ref": "tvdb:123",
  "confidence": 0.94,
  "reason": "title and episode numbering match"
}
```

`ref` is required only for `matched`. `confidence` is meaningful for successful
`matched` and `suppressed` submissions. `reason` is plain debugging text.

`GET /api/releases/v1` returns matched releases when it is given no filters —
the pipeline consumers read it that way and that default does not move. Two
optional parameters widen it for the operator surface:

- `status`: a comma-separated list of match statuses (`status=exhausted,suppressed`).
  Any value outside the closed vocabulary is `400 invalid_request`.
- `maxConfidence`: a float in `(0,1]` adding a strict `confidence < x`, which
  excludes unscored matches — no score is not a low score. The ceiling is the
  caller's policy; the indexer holds no threshold of its own.

Both compose with `ref` and `limit` and keep the newest-first cursor paging.
Neither may be combined with `since`: that is the delta path, which is
matched-only by definition, so the combination is `400 invalid_request`. The
opaque cursor binds the status set and the confidence ceiling exactly as it
binds ref and path, so a cursor replayed under different filters is
`invalid_cursor` rather than a page of some other result set. Every row carries
`matchStatus`, since the list is no longer single-status.

## Source scheduling

Enabled sources run once after the HTTP listener binds and then on their configured
fixed interval. A source loop is sequential, so a slow run cannot overlap the next
tick. `timeout` bounds one run's slice of work; `request_timeout` bounds each page
fetch. Failures are logged and counted, then the loop waits for its next interval;
they do not affect `/healthz`.

Every run walks the listing from page 1 and ingests each page immediately, until
the page's newest plausible timestamp passes `now − settle_window` (or the source's
consecutive-empty floor). The stop rule uses the newest plausible stamp so a
pinned row or an unparseable-date artifact cannot end the walk early, and a run
that fails mid-walk keeps every page already ingested. This is a steady-state
freshness loop, not a backfill engine: after downtime longer than the settle
window, the operator drives `POST /api/releases/v1/sources/{source}/crawl`. Each request
consumes an exact post budget (up to 200), may cross listing-page boundaries,
ingests directly, and returns an opaque `(page, offset)` cursor. The client
threads that cursor until the requested lookback boundary or the confirmed
archive floor; `kura crawl <source> <lookback>` owns this loop automatically
without adding server-side state (see operations.md). Requests share the
scheduled loop's crawler instances, so one fetch gate applies the `max_rps`
ceiling, latency-aware cooldown, and transient retry policy to their combined
upstream traffic. A short per-source page cache lets adjacent chunks that split
a listing page reuse the same snapshot; after it expires, ordinary listing
drift can replay boundary posts, which idempotent ingestion absorbs.

## Queue Semantics

Claiming an item stamps `claimed_at`, sets `lease_expires_at`, and bumps
`claim_token`. A matching submit must echo the current token.

Submitting `matched` or `suppressed` clears the lease and makes the status terminal.
Submitting `unmatched` increments `attempt_count` and keeps the lease in place; the
timeout is the retry mechanic. When the configured failed-attempt cap is reached, an
unmatched result becomes `exhausted`. Expired unmatched rows at or above the cap are
marked exhausted before new claims are offered. Claim crashes do not increment
`attempt_count`.

`PUT /api/releases/v1/{infohash}/status` is the operator path out of a bad or stalled
match and is fenced by an explicit transition table:

| From | To | What it is |
| --- | --- | --- |
| `matched` | `dead` | The torrent proved undownloadable. Terminal. |
| `matched` | `matched` | Affirm or correct an auto-match (requires `ref`). |
| `suppressed` | `matched` | Hand match out of suppression (requires `ref`). |
| `exhausted` | `matched` | Hand match after the matcher gave up (requires `ref`). |
| `exhausted` | `unmatched` | Requeue: hand the release back to the matcher. |
| `exhausted` | `suppressed` | Discard: the operator decides against the release. |

It carries no claim token because it touches no lease: no source state in the table
carries one, since leases exist only on unmatched rows. The endpoint accepts every
label in the closed `MatchStatus` vocabulary as a target — anything else is rejected at
the request boundary with `400 invalid_request` so garbage never reaches SQL. Which
transitions are on offer is entirely the table's: `unmatched → suppressed` has no row,
so the matcher's claim-fenced submit stays the only route out of the queue. A target the
table does not carry from the current status is `409 invalid_transition` naming both ends.

`ref` is required for a transition into `matched` and rejected for every other target.
A ref on a target that takes none is `400 invalid_request` regardless of the row's state,
because it is a malformed body; the ref that `matched` requires is checked *after* the
table, so a transition that was never on offer is reported as the conflict it is rather
than as a complaint about the body. A supplied ref is shape-validated exactly as queue
submit validates one — opaque, never resolved.

Each target owns what it writes. A hand match records the operator's `ref` at confidence
`1.0` (a person deciding by hand is certain by definition — this is not a matching
threshold) and stamps `first_matched_at` if it was unset. A requeue clears `attempt_count`
and the lease columns, which is what puts the release back in front of the claim queue;
without that reset the next claim sweep would re-exhaust it without one new attempt.
`dead` changes only the status, preserving `ref`, `confidence`, and `first_matched_at` —
the match history is why a dead release is not re-selected. Every applied transition
appends one `match_events` row with the target status, the ref and score it recorded, and
the optional `reason`. A request asking for the state the row is already in is an
idempotent no-op that appends no second row; for a hand match that means the same ref
*at* operator confidence, so affirming a low-confidence auto-match still applies.

`GET /api/releases/v1/{infohash}` returns the single-release full context view:
representative release fields, `matchStatus`, nullable derived fields (`magnet`,
`sizeBytes`, `ref`, `confidence`, `firstMatchedAt`), `attemptCount`,
timestamps, `rawItems`, and `matchEvents`. The response deliberately excludes
lease internals (`claimToken`, `claimedAt`, `leaseExpiresAt`). `rawItems` are
ordered by `id ASC`. `matchEvents` are ordered chronologically by `created_at ASC,
id ASC`. Match events are intentionally unpaginated in v1; revisit pagination only
if event counts grow enough to make responses large. Lists stay magnet-free, but
release detail includes `magnet` because it is a single-row full-context lookup
rather than a paged listing.

## Consumer surface

This service serves REST only. Agent-facing MCP tools — `list_releases`,
`get_release`, and `get_magnet` — are served by the suite gateway, which calls
the endpoints above. The gateway owns the tool names, schemas, annotations, and
error projection; nothing here should grow a second copy of them.

The REST `/api/releases/v1/{infohash}/magnet` endpoint returns `{ "infohash": "...", "magnet": "..." }`,
and serves it only while the release is `matched`. A known release in any other status is
`409 not_matched` with the current `matchStatus` in the error `data`; an unknown infohash stays
`404 not_found`. Release detail is unaffected — it is a management surface, not a pipeline one,
so it keeps returning the stored `magnet` regardless of status.
