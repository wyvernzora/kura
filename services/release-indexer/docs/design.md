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
- `POST /api/v1/releases/ingest` remains the external-producer surface; sources still emit the
  same `RawPost` contract and do not import indexer storage. The sanctioned backfill
  producer is the binary's own `crawl` page mode piped into this endpoint.
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
- `exhausted`: too many failed attempts; no longer offered as work.

No `defer`, `escalate`, `reopen`, `next_eligible_at`, recheck state, provenance fields,
or matcher attributes exist in this pass.

## REST API

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/v1/releases` | List matched releases, newest first; optionally narrowed to one ref. |
| `POST` | `/api/v1/releases/ingest` | Accept a batch of crawler posts. |
| `GET` | `/api/v1/releases/{infohash}/magnet` | Get the stored magnet URI for one release. |
| `GET` | `/api/v1/releases/{infohash}` | Get one release detail, raw source evidence, and match history. |
| `POST` | `/api/v1/releases/queue/claim` | Lease claimable unmatched releases. |
| `GET` | `/api/v1/releases/queue/stats` | Return queue/status counts, including exhausted. |
| `POST` | `/api/v1/releases/queue/submit` | Submit `matched`, `unmatched`, or `suppressed` for a claim. |
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

`/api/v1/releases/queue/claim` returns `claimToken`, `attemptCount`, `leaseExpiresAt`, and linked
`rawItems`. `/api/v1/releases/queue/submit` accepts:

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
window, the operator runs the binary's `crawl` page mode — one listing page per
invocation, ingest-ready JSONL out — and pipes it into
`POST /api/v1/releases/ingest` (see operations.md for the recipe).

## Queue Semantics

Claiming an item stamps `claimed_at`, sets `lease_expires_at`, and bumps
`claim_token`. A matching submit must echo the current token.

Submitting `matched` or `suppressed` clears the lease and makes the status terminal.
Submitting `unmatched` increments `attempt_count` and keeps the lease in place; the
timeout is the retry mechanic. When the configured failed-attempt cap is reached, an
unmatched result becomes `exhausted`. Expired unmatched rows at or above the cap are
marked exhausted before new claims are offered. Claim crashes do not increment
`attempt_count`.

`GET /api/v1/releases/{infohash}` returns the single-release full context view:
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

The REST `/api/v1/releases/{infohash}/magnet` endpoint returns `{ "infohash": "...", "magnet": "..." }`.
