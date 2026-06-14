Register a new series in the library at the given metadata ref. Creates a directory under the library root and writes the initial episode spine.

Refuses if the ref is already tracked or the target directory already exists.

<!-- schema-note
Parameter schema is defined in tool_add.go (jsonschema tags on addInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `dirname` (string, optional) — override for the on-disk directory name. Defaults to a sanitized form of the provider's preferred title.
- `ordering` (string, optional) — pin the per-series episode ordering used for the initial spine fetch. One of: `default`, `official`, `dvd`, `absolute`, `alternate`, `regional`. Omit to use the provider's default.
<!-- /schema -->
