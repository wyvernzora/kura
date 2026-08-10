# Release indexer — Operations

For architecture, see [design.md](design.md).

## Build and run

```sh
make build

KURA_RELEASES_DATABASE_URL=postgres://… \
  ./bin/kura-release-indexer --config ./config.example.toml
```

One process serves `/api/releases/v1/ingest`, `/api/releases/v1/sources/{source}/crawl`, `/api/releases/v1/{infohash}/magnet`, `/api/releases/v1/{infohash}`,
`/api/releases/v1/queue/claim`, `/api/releases/v1/queue/stats`, `/api/releases/v1/queue/submit`, and `/healthz` on
`server.addr`, and runs every enabled source crawler. `/metrics` is served on a
second listener, `server.metrics_addr`, and is the only thing on it.

## Configuration

`--config` selects the TOML file and defaults to
`/etc/kura/release-indexer.toml`. The file must exist, unknown keys are rejected,
and invalid configuration fails startup. See
[`config.example.toml`](../config.example.toml) for every field, its requirement,
and its default.

The only runtime secret is required separately:

| Environment variable | Purpose |
| --- | --- |
| `KURA_RELEASES_DATABASE_URL` | PostgreSQL connection URL |

Source tables are optional. An absent table disables that source's scheduled
loop; its on-demand crawl endpoint remains available with defaults. A present
table defaults `enabled` to true and requires `interval` and `settle_window`.
Each enabled source runs once after the HTTP listener binds and then at its
configured interval. Runs for one source never overlap; `timeout` bounds each
run, and `request_timeout` bounds each page fetch (`timeout` must exceed it —
DMHY's deep-history pages have been observed above 60s, hence its larger
per-source defaults of `10m`/`180s`). The URL, request timeout, rate limit, and
cache settings also govern on-demand crawls when `enabled = false`.

Each run walks the listing from page 1 and ingests, page by page, everything
newer than `now − settle_window` — however many pages that is. There is no
cursor, bootstrap, or overlap state; posts older than the settle window are
assumed immutable, and replayed posts are harmless because ingestion is
idempotent. A run that fails mid-walk keeps every page already ingested. A gap
older than the settle window (extended downtime, deep history) is an explicit
backfill — see below.

The container includes a safe example file with sources disabled. Deployments
mount their environment-specific file at `/etc/kura/release-indexer.toml`, normally
from a ConfigMap, and inject the database URL from a Secret.

## Database

The release indexer requires PostgreSQL. Embedded goose migrations run
automatically before the HTTP listener binds. A migration failure aborts startup;
a database already at head is a no-op. The database URL remains in
`KURA_RELEASES_DATABASE_URL`, not TOML.

`database.schema` defaults to `releases`; values must match
`[a-z_][a-z0-9_]{0,62}`. The service creates the schema when it is missing and
explicitly sets it as `search_path` for both the Goose migration connection and the
runtime pgx pool. The role needs `CREATE` on the database only when the schema does
not exist yet; a pre-provisioned schema needs only `USAGE` and `CREATE` on that
schema.

Pointing a populated database at a different schema strands its data: goose finds no
version history in the new schema, migrates a fresh set of tables, and the old data
stays behind — intact, but invisible to the service. To keep it, stop the service
and move it first (owned sequences and indexes follow their tables):

```sql
CREATE SCHEMA IF NOT EXISTS releases;
ALTER TABLE public.releases SET SCHEMA releases;
ALTER TABLE public.raw_items SET SCHEMA releases;
ALTER TABLE public.match_events SET SCHEMA releases;
ALTER TABLE public.goose_db_version SET SCHEMA releases;
ALTER TYPE public.match_status SET SCHEMA releases;
```

## Workflow

```text
release-indexer scheduler -> DMHY / Nyaa
release-indexer crawler   -> direct ingest -> Postgres
external producer        -> POST /api/releases/v1/ingest (escape hatch)
n8n                       -> POST /api/releases/v1/queue/claim
n8n                       -> matcher agent
n8n                       -> POST /api/releases/v1/queue/submit
consumer agent            -> gateway MCP list_releases / get_release / get_magnet
```

`/api/releases/v1/queue/stats.exhausted` is the operator intervention signal for matcher work.

## Backfill

Deep or catch-up backfill restores the standalone crawlers' stateless
count-and-cursor contract, but performs ingestion inside the indexer:

```json
POST /api/releases/v1/sources/dmhy/crawl
{"pageSize":100,"cursor":"","lookback":"260w"}
```

The service walks listing pages until it consumes exactly `pageSize` in-window
posts (default and maximum 200), ingests them, and returns `nextCursor`,
`hasMore`, `stopReason`, timestamp bounds, ingest counters, and queue counts.
The cursor encodes the listing page and row offset but is opaque to clients.
When a 100-post request consumes page 1's 80 rows and page 2's first 20, the
next cursor resumes page 2 at offset 20. The page cache normally reuses the
same page-2 snapshot for the next request.

The CLI owns the bounded cursor loop:

```sh
# Each HTTP request remains one bounded 200-post chunk. The CLI prints every
# committed cursor and continues until the lookback boundary.
KURA_SERVER_URL=https://kura.example.test kura crawl dmhy 30d

# Resume a failed crawl from its last printed checkpoint.
kura crawl dmhy 30d --cursor eyJzb3VyY2UiOiJkbWh5Iiw…

# Use 0 as the lookback to walk to the archive floor.
kura crawl dmhy 0
```

Human output prints each committed cursor to stdout. `--json` emits one raw
JSON object per chunk (JSONL), including `nextCursor`. A failure exits non-zero
and prints an exact resume command to stderr. Pages already ingested remain
committed, and retrying the same cursor is safe because ingestion is
idempotent.

Every source has one shared fetch gate across scheduled and operator crawls.
The configured `max_rps` is a hard ceiling. After a request completes, the
gate also waits at least the rolling mean of the last three request durations
before starting another request; this slows deep SQL-backed pagination even
when its request rate is already low. Network failures, HTTP 408/429, and 5xx
responses retry up to three attempts with exponential backoff (2s initial,
30s cap). A capped `Retry-After` takes precedence when it is longer. Parsing
failures and other 4xx responses do not retry. All waits honor cancellation.

The server resolves a source's consecutive-empty threshold inside a request,
so a cursor is never parked in an unresolved empty run. `hasMore=false` and an
empty cursor mean the lookback boundary or archive floor was positively
observed; `stopReason` says which. A count budget that lands exactly at a page
boundary returns `hasMore=true` because the next page was not fetched; the next
chunk may immediately confirm the floor.

New posts can shift listing rows while a long backfill is running. Within
`cache_ttl`, an adjacent mid-page continuation uses the same snapshot. After
expiry, boundary posts may replay as pages drift; duplicate ingestion is
expected and harmless. The scheduled settle-window crawl heals recent
deletion-driven movement. Parallel cursor chains add no useful coverage. The
shared source gate serializes their upstream fetches, but their independent
cursors still create duplicate ingestion work.

## Security

The service has no application-level auth. Restrict write surfaces by
infrastructure. The pod needs egress to PostgreSQL and DNS. Permit each source
URL that operators may target through either its scheduled loop or on-demand
crawl endpoint. Consumer agents reach this service only through the gateway.

This repo does not ship Kubernetes manifests. Platform policy and the mounted
ConfigMap/Secret belong to the deployment repository.

## Releases

A repo-wide semver tag publishes one
`ghcr.io/wyvernzora/kura/release-indexer` image plus the n8n integration image.
Separate crawler images are no longer built or published.

## Health and shutdown

- `/healthz` remains a DB ping; source-site failures do not make the pod unhealthy.
- `/metrics` exports HTTP, queue, ingest, matcher, and scheduled-source metrics.
  It listens on `server.metrics_addr`, separate from the API. Point the scrape
  at that port and allow only it: the service does not authenticate, so a
  network policy permitting a scrape of a shared port would also permit
  ingest, claim, and submit.
- Startup fails fast if migrations or the HTTP bind fail.
- SIGTERM cancels source crawls, drains in-flight HTTP requests, then closes PostgreSQL.
- Logs are JSON `slog` on stderr.

## Development

```sh
make hooks
make check
make devserver

go test -race ./...
go test -race -tags=conformance ./...
go test -tags=smoke -run TestSmoke ./cmd/kura-release-indexer
```

`make devserver` runs one release-indexer container plus ephemeral PostgreSQL. The
mounted development TOML enables both sources. Stop it with Ctrl-C; use
`docker compose -f tools/devserver/compose.yaml down` to remove the containers.
