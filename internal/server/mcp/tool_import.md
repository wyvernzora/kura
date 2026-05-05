Adopt an existing directory under the library root as a tracked series at the given metadata ref. Companion to `kura_add`, which creates a new directory; use `kura_import` when the directory already exists (operator dropped files in, or `kura_list` returned an `untracked` row).

Refuses if the directory is already tracked or doesn't exist on disk. To re-track a directory whose `.kura/series.json` is corrupted, an operator must use the CLI `--force` form; agents can't bypass.

<!-- schema-note
Parameter schema is defined in tool_import.go (jsonschema tags on importInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `dirname` (string, required) — existing directory basename under the library root to adopt.
- `ordering` (string, optional) — pin the per-series episode ordering used for the initial spine fetch. One of: `default`, `official`, `dvd`, `absolute`, `alternate`, `regional`. Omit to use the provider's default.
<!-- /schema -->
