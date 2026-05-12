Return all known titles and aliases for a series from the metadata provider (TVDB). Each entry carries a BCP-47 language tag; entries without a language tag have an empty `lang` field (common for top-level TVDB aliases).

Use this tool to build effective search terms for release sources like DMHY that index releases under inconsistent or localized names.

## Parameters
- `ref` (string, required) — exact metadata ref for the series, e.g. `tvdb:370070`. Use `kura_resolve` first if you only have a title.

## Response
Returns `ref` and `aliases`: a flat array of `{lang, alias}` objects covering official translated titles and provider-supplied alternate names. Order is not guaranteed.
