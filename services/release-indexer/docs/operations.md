# Release indexer — Operations

For architecture, see [design.md](design.md).

## Build and run

```sh
make build

KURA_RELEASES_DATABASE_URL=postgres://… \
  ./bin/kura-release-indexer --config ./config.example.toml
```

One process serves `/api/v1/releases/ingest`, `/api/v1/releases/{infohash}/magnet`, `/api/v1/releases/{infohash}`,
`/api/v1/releases/queue/claim`, `/api/v1/releases/queue/stats`, `/api/v1/releases/queue/submit`, `/mcp`, and `/healthz` on
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
defaults `enabled` to true and requires `interval`. Each enabled source runs once
after the HTTP listener binds and then at its configured interval. Runs for one
source never overlap; `timeout` cancels the crawl and ingest together.

Each normal run starts at the newest listing and reads at most 200 posts. There is
no cursor, bootstrap, or overlap state. Replayed posts are harmless because
ingestion is idempotent. A gap larger than the recent window is an explicit
backfill; an external producer can still post batches to `/api/v1/releases/ingest`.

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
consumer agent            -> MCP list_releases / get_release / resolve_magnets
```

`/api/v1/releases/queue/stats.exhausted` is the operator intervention signal for matcher work.

## Security

The service has no application-level auth. Restrict write surfaces by
infrastructure. The pod needs egress to PostgreSQL, DNS, and every enabled source
URL. Consumer agents should only reach `/mcp`.

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
- SIGTERM cancels source crawls, drains HTTP/MCP requests, then closes PostgreSQL.
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
