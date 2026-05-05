Drop staged media records on a series — the intent records that `kura_stage` created and `kura_reconcile_apply` would otherwise act on. No files are touched; only the in-library intent is cleared.

Pass `episode` to drop one slot's staged record, `trash`/`extras` to drop specific entries by ULID (from `kura_show`'s `stagedTrash`/`stagedExtras` fields), or `all` to drop every staged record on the series. At least one is required; `all` is mutually exclusive with the others.

Active media records are not affected — use `kura_stage` with `replace=true` followed by reconcile if you want to displace an active record.

<!-- schema-note
Parameter schema is defined in tool_reset.go (jsonschema tags on resetInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `episode` (string, optional) — episode marker (`S01E03`) or storage form (`S01E0003`). Drops the staged record for this slot.
- `trash` ([]string, optional) — ULIDs of `stagedTrash` entries to drop.
- `extras` ([]string, optional) — ULIDs of `stagedExtras` entries to drop.
- `all` (bool, optional) — drop every staged record (episodes + trash + extras) on the series. Mutually exclusive with `episode`/`trash`/`extras`.
<!-- /schema -->
