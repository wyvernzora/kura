Walk the series's directory, parse each media file, refresh provider metadata, and update the per-episode active records. Returns a `jobId`.

Scan can take seconds to minutes (NFS, mediainfo probing) so it runs as a tracked job. The agent can do other work while it finishes.

Files inferred to a slot the provider's spine doesn't have are soft-skipped (kind `metadata_slot_missing`) rather than aborting the whole scan.

Re-submitting on a series with an in-flight scan returns the same `jobId` (deduped at the registry).

<!-- schema-note
Parameter schema is defined in tool_scan.go (jsonschema tags on scanInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `refresh` (bool, optional) — force re-run of mediainfo and source detection on every active record, even when size and mtime are unchanged. A freshly detected `Unknown` source will not overwrite an existing non-Unknown one. Use after fixing filename source tokens or when the on-disk record's source/resolution looks wrong.
- `ordering` (string, optional) — pin the per-series episode ordering and re-fetch the spine under it. One of: `default`, `official`, `dvd`, `absolute`, `alternate`, `regional`. Omit to keep the series's existing ordering.
<!-- /schema -->

## Result fields

`synced[]` — one entry per slot the scan touched:

| status | meaning |
|---|---|
| `added` | New file installed at a slot that was empty. |
| `updated` | Same file at the same slot, contents changed. |
| `unchanged` | No change. |
| `removed` | Previously-recorded file is gone from disk. |

A different-path file for an occupied active slot fails scan instead of replacing the record. Use `kura_stage` with `replace: true` when replacement is intended.

`skipped[]` — files the scan declined to place:

| code | meaning |
|---|---|
| `special_number_not_inferred` | File in specials directory, no `S00Exx` token in name. |
| `episode_number_not_inferred` | No episode number in name. |
| `season_mismatch` | Filename season disagrees with directory season. |
| `ignored_directory` | Excluded subdirectory (e.g. extras). |
| `duplicate_slot` | Multiple files claim the same slot. |
| `metadata_slot_missing` | Filename parses to a slot the provider doesn't have (e.g. file says `S01E25` but season 1 is only 24 episodes). |

`orphanSlots[]` — slots the library still tracks but the provider no longer lists. Informational; usually a metadata revision.

## Interpreting skipped files

For root-level `E01` / `E02` style files skipped as `special_number_not_inferred`, inspect `kura_show` before acting. If the main season is missing and the file count matches, stage `E01..En` to `S01E01..S01En`. Map files to `S00Exx` only when the spine clearly has matching special slots; do not assume root-level means special.

For `metadata_slot_missing` where filenames continue absolute numbering across seasons, inspect the existing season state before asking. If earlier season slots are already present and the provider has later season slots missing, map the absolute file numbers to the missing season slots when the sequence is clear (for example, files `17..24` under `Season 2` map to `S02E05..S02E12` when `S02E01..S02E04` are already present).

If scan returns `synced: []` with no actionable skips, report that no discoverable media was found in the series directory; do not infer files from outside the series.
