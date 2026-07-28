# Changelog

> Note: on 2026-07-23 the repo was consolidated into the kura monorepo
> and all commit subjects were normalized to Conventional Commits. v0.6.0
> is the first release cut from the monorepo; v0.5.1 and earlier predate it
> and refer to the library service's lineage, which the repo-wide version
> line continues.

Notable release changes for Kura.

## Unreleased

Unified API: the suite now presents one hostname, one `/api/v1` REST surface,
and one `/mcp/v1` MCP surface behind a new gateway. This release is **breaking
on every surface** — REST field names, MCP tool names, and the deployment shape
all change, and there is no compatibility layer. Forward-only by design.

### Highlights

- Added `kura-gateway`: Caddy plus an in-Pod MCP bridge plus the SPA in one
  image, serving the whole suite from a single origin. It replaces the
  `kura-webui` image, which is retired.
- Consolidated the MCP surface into one server named `kura` with 16 tools
  spanning both services, replacing the two per-service servers.
- Added `GET /api/v1/releases` and moved the release indexer's REST surface
  under `/api/v1/releases`.
- Added a synthesized `GET /api/v1/health` reporting the gateway and both
  services, and a gateway `/healthz` with no backend dependency.
- Added a root product E2E suite that boots PostgreSQL 18 and every shipping
  service, then exercises cross-service workflows through the gateway.

### Breaking changes

- **Authentication is removed from the services entirely.** There is no bearer
  token, no `[auth]` config table, and no operator tier. A request that reaches
  a service is served. Access control is now solely the authenticating proxy in
  front of the deployment plus a network policy confining the pods — see the
  upgrade notes, because deploying without that boundary exposes destructive
  routes anonymously.
- REST field renames across the library service: `metadataRef` becomes `ref`,
  the former `ref` (the on-disk directory) becomes `directory`, `rows` becomes
  `items`, `dirname` becomes `directory`, `size` becomes `sizeBytes`, and
  `mtime` becomes `modifiedAt`. `POST /api/v1/resolve` moves to
  `POST /api/v1/series/resolve`.
- The release indexer's JSON moves from snake_case to camelCase throughout, and
  its routes move under `/api/v1/releases`: `/ingest`, `/queue/claim`,
  `/queue/stats`, `/submit` (now `/queue/submit`), `/releases/{infohash}`, and
  `/magnets/{infohash}` (now `/{infohash}/magnet`).
- Removed `DELETE /api/v1/series/{ref}` and the three
  `/api/v1/series/{ref}/aliases` routes. Untracking a series is now a
  filesystem operation; alias *storage* is retained and still feeds search.
- Removed the `kura remove` and `kura alias` CLI commands with their endpoints.
- Every MCP tool is renamed: `kura_show` becomes `get_series`, `kura_stage`
  becomes `stage_series_media`, and so on. `resolve_magnets` becomes
  `get_magnet` and now takes a single infohash. `kura_aliases` is removed. No
  compatibility aliases are registered.
- The error envelope is now `{kind, message, data}` on both services. The
  release indexer's `code` field is gone, and its `invalid_input` and
  `no_such_release` kinds become `invalid_request` and `not_found`.
- `list_releases` no longer coerces null `sizeBytes` and `confidence` to zero.
  A release with no recorded size is now distinguishable from one of size zero.
- The release indexer requires a new `server.metrics_addr`, and serves
  `/metrics` only there.
- The library-manager health endpoint moves from `/api/v1/health` to
  `/healthz`; `/api/v1/health` is now the gateway's aggregate suite health.
- n8n workflows must migrate to the type-version 2 nodes, one `Kura API`
  gateway credential, `ref` fields, and camelCase release fields. The package
  does not retain its pre-gateway transports.

### Upgrade notes

- **Put an authenticating proxy in front of the suite before deploying it, and
  confine the pods with a network policy.** The images no longer contain any
  authentication to fall back on, so this is a precondition rather than a
  hardening step.
- Remove any `[auth]` table from `library-manager.toml`. Configuration is
  decoded strictly, so a leftover table now fails startup.
- Set `server.metrics_addr` in `release-indexer.toml`. It is required, must
  differ from `server.addr`, and scrape configuration must point at it — a
  policy allowing a scrape of the shared port previously also allowed the
  unauthenticated write API.
- Point clients at the gateway hostname. The per-service hostnames and the
  separate MCP endpoints are retired.
- Recreate or update n8n workflows against the type-version 2 nodes and replace
  the separate library and release credentials with one `Kura API` credential.

## v0.6.1 - 2026-07-26

### Highlights

- Fixed the web UI image so Caddy starts under a hardened non-root,
  no-new-privileges runtime with all Linux capabilities dropped.
- Added explicit PostgreSQL schema configuration for the release indexer.
  `database.schema` defaults to `releases` and applies consistently to
  embedded Goose migrations and runtime queries.

### Upgrade notes

- Existing release-indexer installations using the `public` schema must move
  their tables, Goose migration history, and `match_status` type into the
  configured schema before starting v0.6.1. See
  `services/release-indexer/docs/operations.md` for the migration procedure
  and required role privileges.

## v0.6.0 - 2026-07-26

First release of the kura suite as a monorepo. The library manager, release
indexer, web UI, `kura` CLI, and n8n nodes now version and publish together
on a single tag line.

### Highlights

- Added the release indexer to the suite: a durable anime release index with
  a work queue, REST and MCP query surfaces, Prometheus metrics, and magnet
  and single-release lookup.
- Absorbed the DMHY and Nyaa crawlers into the release indexer, which now
  runs one non-overlapping scheduled loop per enabled source in process.
  Separate crawler images are no longer published.
- Extracted the `kura` CLI into its own module as a pure REST client,
  discovered through `KURA_SERVER_URL` and authenticated with `KURA_TOKEN`.
- Moved the web UI to a standalone Caddy-served image that proxies `/api/*`
  to the library manager, and dropped the UI embedded in the library
  manager.
- Unified the two n8n packages into a single Kura node with resource and
  action selection, plus a queue trigger.
- Gave each series an archive `generation` counter that advances only when
  archive-relevant content changes — media paths, sizes, mtimes, source,
  adoption attributes, and companion files — and left it untouched by
  provider refreshes, tags, and rescans. Exposed on the show response.
- Published service images under the `ghcr.io/wyvernzora/kura/` namespace:
  `library-manager`, `release-indexer`, `webui`, and `n8n-nodes`.

### Breaking changes

- `kura-library-manager` is configured by a strict TOML file selected with
  `-config`, defaulting to `/etc/kura/library-manager.toml`. Serve settings
  are no longer environment variables; `KURA_TVDB_KEY`, the API token, and
  `KURA_HOST_ID` remain in the environment. The release indexer reads
  `/etc/kura/release-indexer.toml` the same way.
- Binaries renamed to `kura-library-manager` and `kura-release-indexer`.
  The CLI binary is still `kura`, but it ships from its own module and
  reaches the library only over REST — it no longer operates on a local
  library root, apart from the `kura path` command.
- Images moved to the new namespace above; the previous package names are
  no longer published or updated.
- n8n workflows must be rebuilt against the unified Kura node. Credentials
  are now `KuraLibraryApi` and `KuraReleasesApi`.

## v0.5.1 - 2026-07-20

### Highlights

- Fixed inbox listing for exact media-file paths so discovery returns the
  file's `inbox:` selector and metadata instead of rejecting it as a
  non-directory.
- Ensured the n8n Series Show operation always returns `tags` as an array in
  simplified and native output, using `[]` for untagged series.

## v0.5.0 - 2026-07-19

### Highlights

- Added durable series tags across storage, CLI, REST, MCP, n8n, and web
  surfaces, including tag-expression filtering and combined add/remove updates.
- Added responsive series workflow settings for namespaced priority and
  maintenance tags, with priority badges in the library and series views.
- Added an episode details sheet for current and staged media with portable
  path and selector copying, companion files, adoption attributes, media
  comparisons, dimensions, sizes, and modification times.
- Added iOS standalone web-app metadata and viewport behavior for installing
  Kura on the Home Screen.

## v0.4.3 - 2026-07-07

### Highlights

- Fixed n8n `kura show` not-found routing for Axios 404 errors so disabled
  `Error on Not Found` routes missing tracked refs to the untracked output.

## v0.4.2 - 2026-07-07

### Highlights

- Added n8n `kura show` not-found routing with a visible `Error on Not Found`
  toggle, dynamic tracked/untracked outputs, and resolved untracked candidates
  for missing metadata refs.
- Changed the web library's default sort to Last Aired with the latest aired
  series first.

## v0.4.1 - 2026-07-06

### Highlights

- Added Kura logo assets and wired them into the README and n8n custom node
  package.
- Added richer n8n node metadata and icon copying for the Kura node.

## v0.4.0 - 2026-07-06

### Highlights

- Added media extended attributes for active and staged episode records, with
  CLI, REST, MCP, n8n, storage, trash metadata, and e2e parity.
- Added `ALL`, `NONE`, and `AIRING_SEASON` episode selectors for `kura show`
  so agents can request all episodes, metadata-only responses, or the same
  airing-season shorthand used by the library index.
- Ensured n8n `kura show` simplified output includes extended attributes.
- Delayed Renovate PR creation until release-age checks clear, while allowing
  closed age-gated PRs to be recreated after the gate passes.
- Kept Go, web, n8n, Docker, and GitHub Actions dependencies current.

## v0.3.0 - 2026-07-02

### Highlights

- Changed `.kura/index.jsonl` to schema v5 source snapshots so deploy-time
  row policy is applied at read time instead of persisted into row-cache data.
- Added `dateAdded` to `series.json`, defaulting older files from
  `lastScanned`, and exposed Date Added / Last Aired library sorting.
- Fixed index updates after scan plus cold-start/rebuild races around the
  library index.
- Routed the `path` CLI command through the REST server and refreshed the
  README, docs entrypoints, MCP docs, and agent guide for the current
  server-backed flow.

## v0.2.0 - 2026-07-01

### Highlights

- Web UI can add series from search results, preview provider-backed series
  before they are tracked, and flip to the tracked view after add.
- Resolve candidates now include poster artwork from TVDB search results, and
  live series previews report untracked episodes as missing.
- Added release-published Kura n8n custom nodes.
- Added configurable airing-tail handling through `KURA_AIRING_TAIL_DAYS`.
- Kept dependencies current across Go, web, n8n, Docker, and GitHub Actions,
  and stopped release cleanup from pruning GHCR image manifests.

## v0.1.0 - 2026-06-14

Initial release of Kura, an anime-first personal library manager.

### Highlights

- CLI, REST, MCP, and web surfaces backed by one workflow facade.
- TVDB-backed add, import, resolve, show, list, alias, and reindex workflows.
- Scan, stage, reset, trash, remove, and reconcile plan/apply/recover workflows
  for Plex-style anime libraries.
- Agent-oriented MCP contracts with server instructions, structured selectors,
  async job polling, and structured error/output payloads.
- On-disk `.kura` metadata, library index, reconcile/job logs, CAS writes, and
  background sweep support.
- Docker image publishing through GitHub Actions with version stamping,
  multi-arch GHCR images, generated GitHub releases, and best-effort untagged
  image pruning.
