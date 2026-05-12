Return the observed state of one tracked series: metadata header, every season, every episode with its `status` and quality info for active/staged media.

Episode `status`:
- `pending`: air date in the future, no media recorded.
- `missing`: aired, no media recorded.
- `present`: active media is recorded for this episode.
- `staged`: staged media awaiting reconcile. If `active` is also present, reconcile will replace it.

Status reflects persisted state from the most recent `kura_scan`. If a tracked file went missing on disk after the last scan, status still reads `present` until the next scan prunes it. Run `kura_scan` to refresh.

All path fields in this response are scheme-tagged selectors: `series:<rel>` for files inside the series root (e.g. `series:Season 1/Show S01E01.mkv`), `inbox:<rel>` for inbox-staged files. Pass them straight back to `kura_stage` (with `replace=true` to override metadata in place) or `kura_trash` — no scheme reconstruction needed. Applies to `active.file`/`active.companions`, `staged.file`/`staged.companions`, and the staged trash/extras paths.

`stagedTrash` lists files queued for removal at next `kura_reconcile_apply`; `stagedExtras` lists extras queued for placement.

Each episode carries `preferredTitle` (in the operator's first preferred language with fallback to canonical) and `canonicalTitle` (provider's default-language form) when the provider has them. Series-level `artwork.poster.url` is the TVDB CDN URL of the selected series poster (no auth needed to fetch).

**Filters** compose AND across axes:
- `episodes`: `S<N>` (whole season), `S<N>E<E>` (single episode), `S<N>E<A>-<B>` (inclusive range). Specials = `S0`. Missing season errors loudly; empty range overlap returns empty.
- `status`: subset of episode statuses to include.
- `source` / `resolution`: subsets of active media source / resolution to include. Episodes without active media drop when these are non-empty.

**Auto-truncate**: for very long series (1000+ episodes) the response can exceed Claude Code's tool-result token budget. The server drops episode bodies from the spine tail, preserves per-season summaries, and surfaces the dropped slots as selector strings in `truncatedRanges` (e.g. `["S03E15-26", "S04"]`). When `truncated: true`, retry with one of those entries as `episodes` to fetch its detail. `summary` blocks reflect the filtered view so dropped seasons still show their counts.

<!-- schema-note
Parameter schema is defined in tool_show.go (jsonschema tags on showInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `ref` (string, required) — metadata ref to inspect (e.g. `tvdb:370070`). Get one from `kura_resolve`.
- `episodes` (string, optional) — episode selector: `S<N>` | `S<N>E<E>` | `S<N>E<A>-<B>`. Specials = `S0`. Empty = whole series.
- `status` ([]string, optional) — filter to specific episode statuses: `pending`, `missing`, `present`, `staged`, `staged_replacement`. Empty = all statuses.
- `source` ([]string, optional) — filter to specific active-media sources (e.g. `BluRay`, `WebRip`). Empty = all sources.
- `resolution` ([]string, optional) — filter to specific active-media resolutions (e.g. `1080p`, `720p`). Empty = all resolutions.
<!-- /schema -->
