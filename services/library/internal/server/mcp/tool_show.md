Return the observed state of one tracked series: metadata header, season summaries, episodes, and quality info for active/staged media.

All path fields in this response are scheme-tagged selectors that can be passed back to compatible Kura tools without reconstruction. Applies to `active.file`/`active.companions`, `staged.file`/`staged.companions`, and the staged trash/extras paths.

Active and staged media records may include `attrs`, a flat string map written by `kura_stage`. Kura returns attrs verbatim and never filters, sorts, or branches on individual attr keys.

`episodes` accepts `ALL`, `NONE`, `AIRING_SEASON`, `S<N>`, `S<N>E<E>`, or `S<N>E<A>-<B>`. Empty input is `ALL`. `AIRING_SEASON` selects non-special season(s) inside the same airing/tail window used by `kura_list.isAiring`; it still composes with `status`, `source`, and `resolution`.

`stagedTrash` lists files queued for removal at next `kura_reconcile_apply`; `stagedExtras` lists extras queued for placement.

Each episode carries `preferredTitle` (in the operator's first preferred language with fallback to canonical) and `canonicalTitle` (provider's default-language form) when the provider has them. Series-level `artwork.poster.url` is the TVDB CDN URL of the selected series poster (no auth needed to fetch).

**Filters** compose AND across axes:
- `episodes`: `ALL`, `NONE`, `AIRING_SEASON`, `S<N>` (whole season), `S<N>E<E>` (single episode), or `S<N>E<A>-<B>` (inclusive range). Specials = `S0`. Missing explicit seasons error loudly; predicate no-match and empty range overlap return empty.
- `status`: subset of episode statuses to include.
- `source` / `resolution`: subsets of active media source / resolution to include. Episodes without active media drop when these are non-empty.

**Auto-truncate**: for very long series (1000+ episodes) the response can exceed client tool-result token budgets. The server drops episode bodies from the spine tail, preserves per-season summaries, and surfaces the dropped slots as selector strings in `truncatedRanges` (e.g. `["S03E15-26", "S04"]`). When `truncated: true`, retry with one of those entries as `episodes` to fetch its detail. `summary` blocks reflect the filtered view so dropped seasons still show their counts.

<!-- schema-note
Parameter schema is defined in tool_show.go (jsonschema tags on showInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `ref` (string, required) — metadata ref to inspect (e.g. `tvdb:370070`).
- `episodes` (string, optional) — episode selector: `ALL` | `NONE` | `AIRING_SEASON` | `S<N>` | `S<N>E<E>` | `S<N>E<A>-<B>`. Specials = `S0`. Empty = `ALL`.
- `status` ([]string, optional) — filter to specific episode statuses: `pending`, `missing`, `present`, `staged`, `staged_replacement`. Empty = all statuses.
- `source` ([]string, optional) — filter to specific active-media sources (e.g. `BluRay`, `WebRip`). Empty = all sources.
- `resolution` ([]string, optional) — filter to specific active-media resolutions (e.g. `1080p`, `720p`). Empty = all resolutions.
<!-- /schema -->
