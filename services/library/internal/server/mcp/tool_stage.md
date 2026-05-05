Queue a batch of staging changes against one series. Records intent only — files stay in place until `kura_reconcile_apply` runs and moves them into the canonical layout.

Returns a `jobId` immediately; poll `kura_job_status` for terminal state. Multi-item batches with many large files (mediainfo probe per episode item, recursive moves for extras) can take seconds to minutes.

Three input arrays cover the three kinds of placement:

- `episodes[]` — stage media for a spine slot. Each item carries an episode marker, a media path (inbox selector), optional source override, optional companions, and an optional `replace` flag for displacing an existing active or staged record.
- `trash[]` — queue a file (and explicit companions) for trash on the next reconcile. Path must be inside the series root and must not be the active or staged record (or companion of one) for any episode.
- `extras[]` — queue a file or directory tree for placement under `Season N/Extra/[prefix]/<basename>`. Source path can live anywhere on disk. Refuses if the destination already exists.

At least one item across the three arrays is required. Per-item failures (mediainfo probe error, file vanishes mid-flight) appear in `skipped[]` with a stable `code`; whole-batch input violations (duplicate episode, trash/extras invariants) reject the call as `invalid_params`.

Each array (`episodes`, `trash`, `extras`) is capped at 100 items per call. Larger batches reject with `batch_too_large`; split across multiple calls.

<!-- schema-note
Parameter schema is defined in tool_stage.go (jsonschema tags on stageInput, stageEpisodeInputItem, stageTrashInputItem, stageExtraInputItem structs).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `ref` (string, required) — metadata ref (e.g. `tvdb:370070`) from `kura_resolve`.
- `episodes` ([]object, optional) — episode stages. One per spine slot. At least one of `episodes`/`trash`/`extras` is required.
  - `episode` (string, required) — episode marker (`S01E03`) or storage form (`S01E0003`).
  - `media` (string, required) — inbox selector (e.g. `inbox:[BDrip] Show/E03.mkv`). Use `kura_inbox_list` to discover valid values.
  - `source` (string, optional) — override for the source label (`BluRay`, `WebRip`, `Web-DL`, `HDTV`, `DVDRip`, `TVRip`, `Unknown`).
  - `companions` ([]string, optional) — sidecar inbox selectors (subtitles, art) — same shape as `media`.
  - `replace` (bool, optional) — allow staging over an existing active or staged record at this slot.
- `trash` ([]object, optional) — files queued for trash on next `reconcile_apply`.
  - `path` (string, required) — series selector for the file to queue (e.g. `series:Season 1/foo.mkv`). Scoped to the series in the request's `ref`.
  - `companions` ([]string, optional) — sidecar `series:` selectors to drag along into trash.
- `extras` ([]object, optional) — files or directories queued for placement under `Season N/Extra/[prefix]/` on next `reconcile_apply`.
  - `season` (int, required) — season number under which the extra is placed.
  - `source` (string, required) — inbox selector pointing at a file or directory under the inbox root. Use `kura_inbox_list` to discover.
  - `prefix` (string, optional) — optional sub-folder under `Season N/Extra/`.
<!-- /schema -->

## Selectors

Stage inputs never carry absolute filesystem paths — kura runs in its own filesystem namespace and may not see the same paths the agent does. Two schemes:

- `inbox:<rel>` — relative to `KURA_INBOX_ROOT`. Use `kura_inbox_list` to discover available paths.
- `series:<rel>` — relative to the request's series root. Used for trash items (files already in the series directory).

| Field | Scheme | Example |
|---|---|---|
| `episodes[].media` | `inbox:` | `inbox:[BDrip] Show/E01.mkv` |
| `episodes[].companions[]` | `inbox:` | `inbox:[BDrip] Show/E01.en.srt` |
| `extras[].source` | `inbox:` | `inbox:[BDrip] Show/Extras/bts` |
| `trash[].path` | `series:` | `series:Season 1/loser.mkv` |
| `trash[].companions[]` | `series:` | `series:Season 1/loser.en.srt` |

Selector relative paths are forward-slash, NFC-normalized, no leading `/`, no `..` segments.

## Source detection

Always set `source` on episode items, best effort. Parse the filename for tokens (`BluRay`, `BDRip`, `WebRip`, `Web-DL`, `HDTV`, `TVRip`, `DVDRip`). If the filename is silent, look at sibling files in the same release for a hint. If sources genuinely can't be inferred, omit it — Kura will record `Unknown`. Don't guess wildly; a wrong source is harder to spot than `Unknown`.

## Companion discovery

Always discover episode companions. Look in the same directory as the `media` selector (use `kura_inbox_list` with `path=<release-dir>`) for sidecar files matching the media basename — subtitles (`.ass`, `.srt`, `.vtt`, `.ssa`), chapters (`.txt`), cover art (`.jpg`, `.png`), thumbnails, NFO files. Pass them all in `companions` as `inbox:` selectors. Missing companions on stage means they get orphaned when the media moves.

Trash companions are not auto-discovered — pass the sidecar list explicitly. Trash companions are `series:` selectors (sidecar files live alongside the trash target inside the series root).

## Extras

Extras durability is intentionally lower than episodes. No mediainfo probe; no source/resolution recording; no post-reconcile tracking. Once an extra lands in `Season N/Extra/`, kura forgets about it — `kura_show` doesn't list it, `kura_scan` doesn't re-discover it.

The `source` field is an `inbox:` selector pointing at a file (single placement) or a directory (recursive placement). To stage a file already inside the series root as an extra, copy or move it to the inbox first.

## Failure modes

Stage validates the batch in two phases:
- **Phase 1** rejects the whole batch on input shape errors (selector parse failure, duplicate episode, trash invariants, etc.). Error message names the offender. Fix the offending item and re-submit the whole batch.
- **Phase 2** (mediainfo probe failures, mid-flight file vanishes) lets the rest of the batch succeed and reports failures in `skipped[]` with a stable `code`. Re-stage the failed items individually.

Trash invariants: you can't queue a file that's the active or staged record (or companion of one) for any episode. Use `replace: true` on the episode item instead — reconcile will move the displaced active to trash for you.
