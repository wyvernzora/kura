Queue a batch of staging changes against one series. Records intent only — files stay in place until `kura_reconcile_apply` runs and moves them into the canonical layout.

Returns a `jobId`. Multi-item batches with many large files can take seconds to minutes; stage probes episode media, stats trash/extras inputs, and records intent.

Three input arrays cover the three kinds of placement:

- `episodes[]` — stage media for a spine slot. Each item carries an episode marker, a media path selector, optional source override, optional companions, and an optional `replace` flag for displacing an existing active or staged record.
- `trash[]` — queue a file (and explicit companions) for trash on the next reconcile. Path must be inside the series root and must not be the active or staged record (or companion of one) for any episode.
- `extras[]` — queue a file or directory tree for placement under `Season N/Extra/[prefix]/<basename>`. Source must be an `inbox:` selector under the inbox root. Refuses if the destination already exists.

At least one item across the three arrays is required. Per-episode probe failures can appear in `skipped[]` with a stable `code`; whole-batch input violations and missing trash/extras sources reject the call as `invalid_params`.

Each array (`episodes`, `trash`, `extras`) is capped at 100 items per call. Larger batches reject with `batch_too_large`; split across multiple calls.

<!-- schema-note
Parameter schema is defined in tool_stage.go (jsonschema tags on stageInput, stageEpisodeInputItem, stageTrashInputItem, stageExtraInputItem structs).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
<!-- schema -->
## Parameters


- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `episodes` ([]object, optional) — episode stages. One per spine slot. At least one of `episodes`/`trash`/`extras` is required.
  - `episode` (string, required) — episode marker (`S01E03`) or storage form (`S01E0003`).
  - `media` (string, required) — `inbox:` or `series:` selector for the file to stage.
  - `source` (string, optional) — override for the source label (`BluRay`, `WebRip`, `Web-DL`, `HDTV`, `DVDRip`, `TVRip`, `Unknown`).
  - `companions` ([]string, optional) — inbox sidecar selectors. Current MCP parsing accepts companions only for inbox media.
  - `replace` (bool, optional) — allow staging over an existing active or staged record at this slot.
  - `attrs` (object, optional) — flat string map copied verbatim to the media record. Maximum 16 entries; keys must match `[a-z0-9_.]+` and be at most 64 chars; values must be UTF-8 strings with no control chars and at most 1024 chars.
- `trash` ([]object, optional) — files queued for trash on next `reconcile_apply`.
  - `path` (string, required) — series selector for the file to queue (e.g. `series:Season 1/foo.mkv`). Scoped to the series in the request's `ref`.
  - `companions` ([]string, optional) — sidecar `series:` selectors to drag along into trash.
- `extras` ([]object, optional) — files or directories queued for placement under `Season N/Extra/[prefix]/` on next `reconcile_apply`.
  - `season` (int, required) — season number under which the extra is placed.
  - `source` (string, required) — inbox selector pointing at a file or directory under the inbox root.
  - `prefix` (string, optional) — optional sub-folder under `Season N/Extra/`.
<!-- /schema -->

## Selectors

Use `kura_inbox_list` to discover `inbox:` selectors. `series:` selectors are relative to the request's series root.

| Field | Schemes | Example |
|---|---|---|
| `episodes[].media` | `inbox:` or `series:` | `inbox:[BDrip] Show/E01.mkv` or `series:Season 1/E01.mkv` |
| `episodes[].companions[]` | `inbox:` only | `inbox:[BDrip] Show/E01.en.srt` |
| `extras[].source` | `inbox:` | `inbox:[BDrip] Show/Extras/bts` |
| `trash[].path` | `series:` | `series:Season 1/loser.mkv` |
| `trash[].companions[]` | `series:` | `series:Season 1/loser.en.srt` |

Selector relative paths are forward-slash, NFC-normalized, no leading `/`, no `..` segments.

### Series: stages

`episodes[].media` accepts `series:` selectors for files already inside the series root. Two cases:

- **Cross-slot stage.** The series-resident file is moved into the canonical slot for the target episode at reconcile_apply, just like an inbox: stage. Same rules apply: `replace=true` is required if the target slot already has an active or staged record. Current MCP parsing does not accept companions for series-resident episode stages.
- **In-place metadata override.** When the series: path equals THIS episode's own active record path, the stage becomes a metadata-only update — companions are preserved verbatim from the active record; user-supplied companions are forbidden; `replace=true` is required. Reconcile_apply renames the file to its new canonical name and promotes the staged record over the active without trashing anything. Use case: rescue a record whose source/resolution was misidentified by the filename parser (`Unknown` source) by re-staging with an explicit `source` override.

Claimed-path rule for series: stages: media must not be currently tracked as an active or staged record path or companion anywhere in the series. The in-place override is the sole exception, and only for the matching episode's own active record. Reset the conflicting entry (`kura_reset`) or pick a different file.

## Extras

Extras durability is intentionally lower than episodes. No mediainfo probe; no source/resolution recording; no post-reconcile tracking. Once an extra lands in `Season N/Extra/`, kura forgets about it — `kura_show` doesn't list it, `kura_scan` doesn't re-discover it.

The `source` field is an `inbox:` selector pointing at a file (single placement) or a directory. Stage records the intent; reconcile plan expands directory sources and reconcile apply moves them. To stage a file already inside the series root as an extra, copy or move it to the inbox first.

## Failure modes

Stage validates the batch in two phases:
- **Phase 1** rejects the whole batch on input shape errors (selector parse failure, duplicate episode, trash invariants, etc.). Error message names the offender. Fix the offending item and re-submit the whole batch.
- **Phase 2** (mediainfo probe failures, mid-flight file vanishes) lets the rest of the batch succeed and reports failures in `skipped[]` with a stable `code`. Re-stage the failed items individually.

Trash invariants: you can't queue a file that's the active or staged record (or companion of one) for any episode. Use `replace: true` on the episode item instead — reconcile will move the displaced active to trash for you.
