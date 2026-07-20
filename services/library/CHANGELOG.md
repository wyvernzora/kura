# Changelog

Notable release changes for Kura.

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
