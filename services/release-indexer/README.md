<div align="center">
    <br>
    <br>
    <img width="256" src="docs/assets/logo-full-256.png">
    <h1 align="center">宅配</h1>
</div>

<p align="center">
<b>kura release-indexer - self-hosted anime release indexer</b>
</p>

<hr>
<br>
<br>

The **release-indexer** (宅配 — "home delivery / courier"; formerly the standalone
takuhai project) is the kura suite's anime **release index**: a durable store +
source crawler + work queue + query API. It crawls configured sources, keeps their
raw releases immutably, dedups them by infohash into a queryable catalog, and
exposes the unmatched ones as a work queue.

It pairs with the kura library manager (蔵 — "storehouse") in the same suite.

## The idea

The hard direction of release management — *canonical series → search keywords* — is
intractable; the forward direction — *raw release name → canonical series* — is not.
The release-indexer inverts the problem: an **external matching agent** resolves each
release to a canonical ref once, at ingest. Consumers that already hold a ref then
query the index **directly by ref**, no keyword guessing.

The release-indexer holds **no matching intelligence** of its own. Canonical refs are
opaque strings it never resolves; it only records the matcher outcome.

## Architecture in one breath

The release-indexer runs DMHY and Nyaa crawls on configured intervals and ingests
their posts directly. Each run starts at the newest listing and ingests, page by
page, everything newer than the source's configured settle window; durable
ingestion makes replay harmless.
`POST /api/v1/releases/ingest` remains the external-producer surface.
`POST /api/v1/sources/{source}/crawl` restores the original stateless
count-and-cursor crawler contract for backfills, except the indexer now ingests
each chunk directly instead of returning posts for an out-of-process shuffle.
The `kura crawl <source> <lookback>` client owns the bounded cursor loop and
prints resumable checkpoints (see docs/operations.md).
n8n drives only the **match loop** over the queue
REST API; a stateless matcher resolves each release. Consumers read the catalog over
a REST API under `/api/v1/releases`. Postgres is both
the store and the work queue. See [docs/design.md](docs/design.md).

## Quick start

```sh
make devserver

make build
KURA_RELEASES_DATABASE_URL=postgres://… \
  ./bin/kura-release-indexer --config ./config.example.toml
```

The binary runs its migrations on startup. Non-secret settings, including the
PostgreSQL schema, live in a strict TOML file; the database URL remains
Secret-backed. See
[docs/operations.md](docs/operations.md) for deployment and the container build.

## Documentation

- [docs/design.md](docs/design.md) — architecture, data model, queue semantics,
  external contracts, invariants.
- [docs/operations.md](docs/operations.md) — build, configure, deploy, run, observe.
- [docs/conformance-matrix.md](docs/conformance-matrix.md) — the spec-clause → test map.

## Development

```sh
make hooks    # point git at .githooks/ (commit-message guard)
make check    # fmt + vet + lint + test + build
```

DMHY and Nyaa live under `sources/` in the same Go module as the service.

## License

MIT — see [LICENSE](LICENSE).
