# AGENTS.md — kura monorepo

Umbrella operating rules for coding agents. Read the user-global rules
first (`~/.agents/AGENTS.md`, `~/.agents/go.md`). Service-specific
context, learnings, and overrides live in each service's own AGENTS.md:

- `services/library/AGENTS.md` — the core library manager (was the kura repo).
- `services/releases/AGENTS.md` — the release indexer (was the takuhai repo).

## Layout

| Path | What it is |
|---|---|
| `services/library/` | kura core: library manager, REST + MCP + embedded web UI |
| `services/releases/` | release indexer + `sources/{dmhy,nyaa}` crawler modules |
| `integrations/n8n/{kura,releases}/` | the two n8n node packages |
| `prompts/` | reserved: versioned agent/matcher prompts |
| `deploy/` | reserved: deployment manifests |

One `go.work` spans all four Go modules. `make check` at the root fans
out to per-service Makefiles; prefer per-service `make` during iteration.

## Commit convention (enforced)

Conventional Commits v1.0.0, subject ≤72 chars, enforced by
`.githooks/check-commit-subject.sh` via lefthook and CI:

- Types: feat, fix, docs, refactor, test, build, ci, chore, perf, revert.
- Scopes (closed enum): `library`, `indexer`, `backup`, `webui`, `repo`,
  `deps`, `release`, `n8n`, `deploy`. Scope says where; type says what
  kind. Adding a scope is a deliberate one-line change to the hook.
- Merge commits and `fixup!`/`squash!` markers are exempt.

## Versioning and artifacts

- Repo-wide versioning: one `vX.Y.Z` tag line for the whole monorepo,
  continuing the original kura lineage. The release workflow builds every
  service image at that version.
- Directories and commit scopes are unprefixed; binaries and images keep
  the `kura-` prefix (`kura-releases`, `ghcr.io/wyvernzora/kura-server`,
  …) because they leave the repo's namespace.
