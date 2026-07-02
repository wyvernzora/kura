# Changelog

Notable release changes for Kura.

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
