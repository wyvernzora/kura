Look up metadata candidates for a series. Accepts free-text title fragments or one explicit metadata ref (e.g. `tvdb:370070`).

Returns a `candidates` array. Cardinality:
- 0: no match.
- 1: unique.
- 2+: ambiguous.

Each candidate carries `evidence` (which term matched, where, with qualifying annotations like `full_match`) for ranking heuristics. Empty for explicit-ref lookups.

Use `genres` + `originalLanguage` + `originalCountry` to distinguish among candidates that share a title — e.g. an anime adaptation typically tags `Animation` (or `Anime`) and `originalLanguage=ja`, while a live-action adaptation of the same source omits the Animation genre.

<!-- schema-note
Parameter schema is defined in tool_resolve.go (jsonschema tags on resolveInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `terms` ([]string, required) — selector terms. Each is either a free-text title fragment matched against series titles in any source language (e.g. `bookworm`, `honzuki`) or one explicit metadata ref (e.g. `tvdb:370070`). Must not be empty.
<!-- /schema -->

## Candidate fields

When `candidates` returns 2+ entries, each candidate includes structured fields for disambiguation.

| Field | Use it for |
|---|---|
| `genres` | `Animation` / `Anime` → animated adaptation; absence → usually live-action. |
| `originalLanguage` | `ja` for Japanese productions, etc. |
| `originalCountry` | `JP`, `KR`, `US`, etc. |
| `year`, `firstAired` | Distinguish sequels, remakes, spinoffs. |
| `evidence` | Per-term match info for overlapping titles. |
