# Changelog

Notable release changes for Kura.

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
