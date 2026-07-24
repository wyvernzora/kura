# Kura docs

Engineer-facing reference for Kura. The project [README](../README.md) is the
short homepage; this directory holds the operational detail.

## Reading order

If you are new to Kura, read in this order:

1. [Concepts](concepts.md) — vocabulary, domain model, episode state
   derivation, naming convention, internal invariants.
2. [Lifecycle](lifecycle.md) — every workflow with edge cases:
   add, import, scan, stage, replace, reset, plan, apply, recovery,
   trash, bootstrap, remove.
3. [Deployment](deployment.md) — start the single writer, configure strict
   TOML, roots, and bearer-token auth.
4. The surface you intend to use:
   [CLI](cli.md) · [REST API](rest-api.md) · [MCP](mcp.md).
5. [Storage](storage.md) — on-disk file formats:
   `index.jsonl`, `series.json`, reconcile plan JSONL, trash
   `meta.json`.

## Index

| Doc | What it covers |
|---|---|
| [concepts.md](concepts.md) | Actors, vocabulary (MetadataRef, SeriesRef, EpisodeRef, spine, holder, mutator, CAS, ULID), domain model, episode state, series resolution, naming convention + sanitization, internal invariants, hard invariants, jobs, out of scope. |
| [lifecycle.md](lifecycle.md) | The core workflows in narrative form, recovery and surgery matrix, async job model, trash management. |
| [cli.md](cli.md) | Server-backed CLI usage, operations catalog, selectors, every `kura <verb>` flag, and the TOML/env boundary. |
| [rest-api.md](rest-api.md) | Auth, CORS, operator gating, ETag, full endpoint catalog, async job protocol, version surfacing. |
| [mcp.md](mcp.md) | 14 MCP tools, agent safety properties, disambiguation. |
| [deployment.md](deployment.md) | Single-writer rule, Alpine image, build args, strict TOML, secret env vars, bootstrap, stuck-claim recovery, k8s health probes, runtime UID overrides. |
| [storage.md](storage.md) | Layout, conventions, `series.json` v3, `index.jsonl` v5, reconcile plan JSONL v2, trash `meta.json` v1, per-job logs. |

## Contributing

Coding agents and human contributors should read
[AGENTS.md](../../../AGENTS.md) first. It covers the operating ground rules
(no flattery, no fabrication, surgical changes), the project's
guiding principles, and the Go engineering conventions.
