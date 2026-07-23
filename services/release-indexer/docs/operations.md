# Release indexer — Operations

For architecture, see [design.md](design.md).

## Build and run

```sh
make build

KURA_RELEASES_DATABASE_URL=postgres://… \
  ./bin/kura-release-indexer --config ./config.example.toml
```

One process serves `/ingest`, `/magnets/{infohash}`, `/releases/{infohash}`,
`/queue/claim`, `/queue/stats`, `/submit`, `/mcp`, `/healthz`, and `/metrics`, and
runs every enabled source crawler.

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
backfill; an external producer can still post batches to `/ingest`.

The container includes a safe example file with sources disabled. Deployments
mount their environment-specific file at `/etc/kura/release-indexer.toml`, normally
from a ConfigMap, and inject the database URL from a Secret.

## Database

The release indexer requires PostgreSQL. Embedded goose migrations run
automatically before the HTTP listener binds. A migration failure aborts startup;
a database already at head is a no-op.

## Workflow

```text
release-indexer scheduler -> DMHY / Nyaa
release-indexer crawler   -> direct ingest -> Postgres
external producer        -> POST /ingest (escape hatch)
n8n                       -> POST /queue/claim
n8n                       -> matcher agent
n8n                       -> POST /submit
consumer agent            -> MCP list_releases / get_release / resolve_magnets
```

`/queue/stats.exhausted` is the operator intervention signal for matcher work.

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
