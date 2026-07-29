# Release indexer — Operations

For architecture, see [design.md](design.md).

## Build and run

```sh
make build

KURA_RELEASES_DATABASE_URL=postgres://… \
  ./bin/kura-release-indexer --config ./config.example.toml
```

One process serves `/api/v1/releases/ingest`, `/api/v1/releases/{infohash}/magnet`, `/api/v1/releases/{infohash}`,
`/api/v1/releases/queue/claim`, `/api/v1/releases/queue/stats`, `/api/v1/releases/queue/submit`, and `/healthz` on
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

Source tables are optional. An absent table disables that source. A present table
defaults `enabled` to true and requires `interval` and `settle_window`. Each
enabled source runs once after the HTTP listener binds and then at its configured
interval. Runs for one source never overlap; `timeout` bounds each run, and
`request_timeout` bounds each page fetch (`timeout` must exceed it — DMHY's
deep-history pages have been observed above 60s, hence its larger per-source
defaults of `10m`/`180s`).

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
external producer        -> POST /api/v1/releases/ingest (escape hatch)
n8n                       -> POST /api/v1/releases/queue/claim
n8n                       -> matcher agent
n8n                       -> POST /api/v1/releases/queue/submit
consumer agent            -> gateway MCP list_releases / get_release / get_magnet
```

`/api/v1/releases/queue/stats.exhausted` is the operator intervention signal for matcher work.

## Backfill

Deep or catch-up backfill is operator-scripted, not automated: the binary's
`crawl` subcommand fetches **one listing page per invocation** with the same
config, fetchers, and parsers as the service, and prints ingest-ready JSONL —
each stdout line is exactly the element `POST /api/v1/releases/ingest` accepts
in `posts[]`. The resume cursor (`next_page=N`) goes to stderr so stdout stays
pipeline-pure; an empty page (archive floor / past the end) prints nothing and
exits 0.

```sh
kura-release-indexer crawl -config /etc/kura/release-indexer.toml -source nyaa -page 3
```

A full catch-up is a shell loop — resumable at any page, paced by its own
`sleep`, with live progress from the ingest response counters:

```bash
#!/usr/bin/env bash
set -euo pipefail

KURA="http://127.0.0.1:8080"
CUTOFF="2026-05-01"                                             # your cutoff
page=1
while :; do
  # Empty stdout means the archive floor ONLY when the crawl succeeded; a
  # failed fetch/parse also writes nothing, so branch on exit status first.
  if ! kura-release-indexer crawl -config cfg.toml -source nyaa -page "$page" > batch.jsonl; then
    echo "crawl failed on page $page; fix the cause, then resume at page=$page" >&2
    exit 1
  fi
  if [ ! -s batch.jsonl ]; then
    # The in-process walk requires TWO consecutive empty pages before calling
    # the archive floor; a single empty page can be a listing artifact. Fetch
    # one more page to confirm before trusting a first empty.
    echo "empty page at $page — likely the archive floor; confirm with page $((page+1)) if in doubt" >&2
    break
  fi
  # --fail-with-body + pipefail: an ingest error envelope must stop the loop
  # rather than silently advancing past an unpersisted page.
  if ! jq -s '{posts:.}' batch.jsonl \
      | curl -sS --fail-with-body -XPOST "$KURA/api/v1/releases/ingest" -d @- \
      | jq -c .batch; then
    echo "ingest failed for page $page; resume at page=$page" >&2
    exit 1
  fi
  # Cutoff on the batch MAXIMUM: stop only when even the newest post on the
  # page is past the cutoff. A minimum would let one pinned/epoch-dated row
  # end a deep backfill after page 1.
  newest=$(jq -rs 'map(.publishedAt) | max' batch.jsonl)
  if [[ "$newest" < "$CUTOFF" ]]; then break; fi
  page=$((page+1)); sleep 2                                     # politeness
done
```

The exit-status branches are load-bearing: this loop is the only backfill
mechanism, and a 503, a request timeout, or a markup change all produce the
same empty stdout as a genuine archive floor. Without them a truncated
backfill reports itself as a clean completion.

Every step is idempotent: re-running a page, overlapping ranges, or restarting
the loop mid-way is always safe. Run it after any outage longer than the
settle window ("was the crawler down for more than N? run the catch-up loop"),
and for one-time deep history imports pick the cutoff accordingly — on DMHY,
deep pages are slow on the source side, which paces the loop naturally. The
subcommand needs no database access and works on a configured-but-disabled
source. Politeness is your loop's `sleep` — the subcommand makes exactly one
upstream request per invocation, so `max_rps` never has a second request to
delay; do not drop the sleep or fan invocations out in parallel.

## Security

The service has no application-level auth. Restrict write surfaces by
infrastructure. The pod needs egress to PostgreSQL, DNS, and every enabled source
URL. Consumer agents reach this service only through the gateway.

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
