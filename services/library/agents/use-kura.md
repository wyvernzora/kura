---
name: use-kura
description: Operate a Kura anime/TV library through its MCP tools. Triggers on requests to list, inspect, add, import, scan, stage, reconcile, or organize shows; adopt untracked directories; resolve duplicate episodes; or recover from staging or reconcile failures.
---

# Kura library management — agent guide

You manage an anime/TV library through Kura's MCP tools. This document
tells you what each tool does to the library, when to use it, and how
to chain them.

Per-tool input/output schemas live in each tool's MCP description,
served at runtime; read the description before first use.

---

## 1. What the library contains

A **library** is a flat collection of **series**. Each series:

- has a stable **metadata ref** like `tvdb:370070` (the primary handle
  for every tool except `kura_resolve`),
- carries provider info (titles, air dates, season/episode list),
- has zero or more **episode slots** — one per episode the provider
  knows about,
- and may have a **media file** installed in each slot.

A series is either **tracked** (Kura is managing it) or **untracked**
(a directory exists but Kura was never told what it is).

**Episode slot states the agent cares about:**

- `present` — file recorded as installed at last scan.
- `staged` — a file is *queued* to be installed at this slot,
  awaiting reconcile.
- `staged_replacement` — staged file will replace the
  currently-present one.
- `missing` — aired episode, no file recorded.
- `pending` — episode hasn't aired yet.

`kura_show` reads persisted metadata only — it does not probe the
filesystem. If a file disappeared after the last scan, the slot still
reads `present`. Run `kura_scan` to reconcile disk state back to
metadata.

**Series-level rollups (`kura_list`):**

- `complete` — every aired episode is present.
- `incomplete` — at least one aired episode is missing.
- `airing` — current episodes present, future ones still pending.
- `untracked` — directory not registered with Kura.
- `error` — couldn't read the series.

Episode markers use the form `S01E03`. Both `S01E03` and `S01E0003`
are accepted as input.

---

## 2. Tool catalog

| Tool | Effect on library |
|---|---|
| `kura_resolve` | None. Looks up metadata refs from titles. |
| `kura_list` | None. Returns one page of series with summary state. Paginated — default 100 rows/page, max 1000. See §12. |
| `kura_show` | None. Returns full state of one series. |
| `kura_inbox_list` | None. Lists files under `KURA_INBOX_ROOT` so you can discover what's available to stage. Plain-text output, mtime-desc sorted. See §4.d + §13. |
| `kura_add` | Creates a new tracked series. Empty directory. |
| `kura_import` | Marks an existing untracked directory as tracked. |
| `kura_stage` | Queues a batch of staging changes against one series: episode stages, trash items, extras placements. Files are **not** moved until reconcile. **Async.** See §4.d. |
| `kura_reset` | Cancels queued stages by episode marker, trash ULID, extras ULID, or `--all`. No file change. |
| `kura_reconcile_plan` | None visible. Computes what `reconcile_apply` would do, including stagedTrash + stagedExtras. |
| `kura_reconcile_apply` | Moves staged files into place: episodes to canonical slots, stagedTrash to `.kura/trash/`, stagedExtras to `Season N/Extra/[prefix]/`. Displaces old files to trash. **Async.** |
| `kura_scan` | Re-reads the series's directory and updates which file is recorded for each slot. **Async.** |
| `kura_job_status` | None. Polls an async job. |

**Async tools** return a `jobId` immediately. Poll `kura_job_status`
until the job's `state` is terminal (`succeeded`, `failed`, or
`cancelled`). Don't assume a job succeeded without polling.

---

## 3. Operating rules

1. **Default to autonomy.** When you encounter an issue (duplicate
   slots, missing source token, wrong-subdir file, mis-numbered
   episode), try to fix it using the available tools. Only escalate
   when you cannot articulate a clear reason for the choice you would
   make.
2. **Always have an articulable reason.** Before mutating anything,
   you must be able to state in one sentence why this action is the
   right one. "BluRay 1080p beats WebRip 720p" is a reason. "It feels
   right" is not. No reason → surface to user.
3. **Be consistent.** When the same kind of choice recurs (e.g.
   multiple duplicate slots across the same series, multiple release
   groups providing copies), apply the same criterion across the whole
   set. Don't pick Group A for E01 and Group B for E02 unless there's
   an explicit per-slot reason.
4. **Always get a metadata ref from `kura_resolve` first** unless you
   already have one from `kura_list` / `kura_show`. Never invent or
   guess refs.
5. **Tools that take `ref` accept exactly one ref**, not a title.
6. **`kura_resolve` cardinality drives behavior:** 0 → no match,
   surface to user. 1 → safe to act. 2+ → disambiguate before acting
   (see §5).
7. **After `kura_scan` or `kura_reconcile_apply`**, poll the job
   until terminal. Treat `failed` like any other tool error.
8. **Ask before mutating** only when rules 1–3 don't yield a clear
   answer. Adding, importing, staging, reconciling, and resetting all
   change the library. Resolve / list / show / job_status are
   read-only and free to call.
9. **Don't echo the user's inputs as new info.** Tool responses
   already drop fields the caller passed in. Surface what's *new*:
   discovered files, statuses, conflicts, decisions you made.

---

## 4. The five recurring workflows

### 4.a Triage

```
kura_list                                 # page 1 (default 100 rows)
kura_list(cursor=<nextCursor>)            # repeat until nextCursor is absent
```

Walk every page when triaging the whole library — one page is **not**
the whole library. Stop when the response omits `nextCursor`. If a
page returns `dataChanged: true`, the index mutated between pages;
finish the walk, then re-list affected groupings if strict counts
matter (see §12).

For narrow asks (e.g. "show me errors"), pass `statuses=["error"]`
so you don't paginate through unrelated rows. Prefer
`kura_show(ref)` over re-listing for a single known series.

Group rows by `status`. Recommended attention order:

1. `error` — fix or surface to user.
2. `incomplete` — investigate per series with `kura_show`.
3. `untracked` — adopt with §4.b.
4. `airing` — informational.
5. `complete` — leave alone unless user wants upgrades.

### 4.b Adopt an untracked directory

```
kura_resolve(terms=[<dirname or title>])
# pick the right candidate — see §5
kura_import(ref=<chosen>, dirname=<existing dirname>)
kura_scan(ref=<chosen>) → jobId
kura_job_status(jobId)
```

Inspect the scan result for `synced` (slots populated) and `skipped`
(files Kura couldn't place — see §6).

### 4.c Add a new series the user is acquiring

```
kura_resolve(terms=[<title>])
kura_add(ref=<chosen>)         # creates the directory
# user drops files in over time
kura_scan(ref=<chosen>) → jobId
kura_job_status(jobId)
```

### 4.d Stage media for a series

`kura_stage` queues a batch of changes against one series in a single
async call. The batch can mix episode stages (`episodes[]`), trash
queues (`trash[]`), and extras placements (`extras[]`). See the tool
description for selector syntax, source detection, companion
discovery, extras durability, and failure modes.

```
# Discover what's in the inbox first.
kura_inbox_list(path="[BDrip] Show Title")
# → copy the inbox:<rel> selector you want to stage.

jobId = kura_stage(
  ref=<series>,
  episodes=[{episode: "S01E03",
             media: "inbox:[BDrip] Show Title/E03.mkv",
             source: "BluRay",
             companions: ["inbox:[BDrip] Show Title/E03.en.srt"],
             replace: false}],
  trash=[{path: "series:Season 1/loser1.mkv"},
         {path: "series:Season 1/loser2.mkv"}],
  extras=[{season: 1,
           source: "inbox:[BDrip] Show Title/Extras/bts-folder",
           prefix: "behind-the-scenes"}],
)
kura_job_status(jobId)                          # poll until terminal

kura_reconcile_plan(ref=<series>) → {token, plan}
kura_reconcile_apply(ref=<series>, token=<token>) → jobId
kura_job_status(jobId)
kura_show(ref=<series>)                         # verify
```

Single-item calls are valid — most of the time you stage one episode
at a time. The batch shape exists for atomic "winner + losers"
workflows (see §7).

To abort before reconcile:
`kura_reset(ref, episode=<marker> | trash=[<ulid>...] | extras=[<ulid>...] | all=true)`.

### 4.e Refresh after the user changed the directory

User added, removed, or renamed files; or wants updated provider info
applied.

```
kura_scan(ref=<series>) → jobId
kura_job_status(jobId)
```

Examine `synced` rows (statuses `added` / `updated` / `unchanged` /
`removed` / `replaced`) and `skipped`.

When the goal is to adopt specific files in an already-tracked
series, prefer `kura_stage` + `kura_reconcile_plan` /
`kura_reconcile_apply` over relying on a re-scan to adopt them. Use
scan to discover or refresh facts; use stage/reconcile for
intentional placement.

---

## 5. Disambiguating `kura_resolve`

See the `kura_resolve` tool description for candidate field meanings
and disambiguation heuristics.

---

## 6. Reading scan results

See the `kura_scan` tool description for `synced[]` / `skipped[]` /
`orphanSlots[]` field tables and skip-interpretation rules.

---

## 7. Duplicate slots

When two files claim the same `(season, episode)`, scan reports both
as `skipped` with `duplicate_slot` and **leaves the slot empty**.
Kura does not pick a winner — that's your job.

Per-slot ranking (highest first):

1. **Source.** BluRay > WebRip ≈ Web-DL > HDTV ≈ TVRip ≈ DVDRip >
   Unknown.
2. **Resolution.** More pixels wins (1080p > 720p > 480p).
3. **File size.** Same source + resolution → larger usually means
   higher bitrate. Treat ties within ~10% as "no preference".

**When duplicates span the series** (multiple release groups have
provided full or partial sets), pick at the *release-group level*
before going slot by slot:

1. Identify the distinct release groups / sources present (filename
   tokens, source folders, naming conventions).
2. Pick the group whose set is **most complete** at the **highest
   quality**. A full BluRay 1080p run beats a half-finished BluRay
   1080p remux paired with WebRip fillers.
3. Apply that group's files to every duplicate slot. Don't mix.
4. Only fall back to per-slot ranking when the chosen group is
   missing an episode the alternate group has.

Stage each chosen file (see §4.d). Set `source` from the filename,
pull in companions. Articulate the choice in your response: "Picked
the [Group X BluRay 1080p] release across S01; falls back to [Group
Y WebRip 1080p] for E07 which Group X is missing."

**Atomic winner + losers in one batch.** Bundle the winner stage and
the loser trashes into a single `kura_stage` call. The winner comes
from inbox (`inbox:` selector); the losers are already inside the
series root (`series:` selector):

```
kura_stage(
  ref=<series>,
  episodes=[{episode: "S01E03",
             media: "inbox:[GroupX BluRay 1080p]/E03.mkv",
             source: "BluRay", ...}],
  trash=[
    {path: "series:Season 1/loser-group1.mkv"},
    {path: "series:Season 1/loser-group2.mkv"},
  ],
)
```

One `reconcile_apply` then promotes the winner and sweeps the losers
to trash atomically.

If you've already staged the winner separately and only need to trash
losers, `kura_stage(trash=[...])` adds them to the next reconcile.
There is no standalone trash tool — all agent-driven trashing flows
through `kura_stage` + `kura_reconcile_apply`.

Trash invariants (see §10): you can't queue a file that's the active
or staged record (or companion of one) for any episode — use
`kura_stage` with `replace: true` instead, and reconcile will move
the displaced active to trash for you.

Escalate to the user only when:

- Rankings genuinely tie (same source, same resolution, similar
  size) and you can't articulate a tiebreaker, or
- The files are something you can't classify (no source/resolution
  tokens, opaque filenames), or
- The release groups are visibly different in cohesion / quality but
  neither has an obvious advantage (e.g. one is older but stable,
  one is newer but partial).

---

## 8. Stage vs scan — picking the right tool

- **Scan** is for files *already in the series directory*, named
  conventionally. Bulk discovery and refresh.
- **Stage + reconcile** is for any case scan can't (or shouldn't)
  handle on its own:
  - File lives in the inbox (`KURA_INBOX_ROOT`) and you want to
    install it. Discover via `kura_inbox_list`, stage with `inbox:`
    selector.
  - File is in the series directory but in the wrong subdirectory or
    with a non-canonical name. (Scope-limited today: stage media
    must come from the inbox, so to relocate an in-library file
    you'd copy it to inbox first, stage it, then trash the misplaced
    original via `series:` selector in the same batch.)
  - One of several duplicate-slot files in the series directory
    needs to be picked. Trash the losers via `series:` selectors;
    if the winner also needs to come from inbox, stage it as
    `inbox:` in the same batch.
- Don't stage a file that's already in place and well-named — scan
  handles that.
- Don't expect scan to pull in files from outside the series
  directory — it doesn't walk those.
- Don't expect stage to ingest a file from a location kura can't
  see. Selectors are scoped to the inbox or the series root;
  arbitrary host paths aren't valid.

---

## 9. Reconcile plan and apply

`kura_reconcile_plan` computes what `reconcile_apply` would do and
returns a `token`. Nothing changes on disk. `kura_reconcile_apply`
consumes the token and performs the moves.

See each tool's description for field semantics, expiry handling,
verification, and busy-recovery guidance.

---

## 10. Common failures

| Symptom | Likely cause | Action |
|---|---|---|
| `kura_resolve` returns 0 | Title romanization mismatch or provider gap. | Try alternate titles; ask the user for an explicit ref if they have one. |
| `kura_scan` skips good-looking files with `metadata_slot_missing` | Provider numbers seasons differently than the user's filenames. | Inspect with `kura_show`; user may need to renumber files or stage explicitly. |
| Source column shows resolution string (e.g. `1920x1080`) | Filename suffix has only resolution, no source token. Records show `Unknown` correctly on fresh scans of recent binaries. | Re-scan; if still wrong, override via `kura_stage(source=...)`. |
| `kura_reconcile_plan` shows the active or staged file as `Unknown` source | Kura's filename parser missed the source token on the originally-staged file (uncommon naming, parser gap). | Check the **original** filename (before kura's rename) for a source token (`BluRay`, `WebRip`, `WEBDL`, `HDTV`, etc.). If present, re-stage the affected file with an explicit source override using a `series:` media selector (in-place override): `kura_stage` episode item with `media: "series:<active-path>"`, `replace: true`, `source: "<token>"`, no companions. Reconcile_apply rewrites the persisted source and renames the file to its corrected canonical filename without moving it through the inbox. |
| `kura_stage` errors with episode-already-exists | Slot already has a recorded file at a different path. | Confirm with user, then re-call with `replace: true` on the episode item. |
| `kura_stage` rejects with "expected inbox: scheme" / "expected series: scheme" | Selector type wrong for the field. Episode `media` + companions + extras `source` need `inbox:` selectors; trash `path` + companions need `series:` selectors. | Re-build the selector with the right scheme. |
| `kura_stage` rejects with "missing scheme" / "selector escapes root" / "leading slash not allowed" | Bare path or absolute path passed where a selector was expected. Selectors look like `inbox:<rel>` or `series:<rel>`, never `/abs/path`. | Discover the right selector via `kura_inbox_list`; rebuild and retry. |
| `kura_inbox_list` returns `path does not exist` | Subpath doesn't exist under `KURA_INBOX_ROOT`. | List the parent dir; correct the path. |
| `kura_stage` rejects whole batch with `invalid_params` (Phase 1) | Trash invariant violated (path is active record / companion / inside a staged record), duplicate episode in batch, duplicate path across batch, or extras destination collision. Error message names the offender. | Fix the offending item and re-submit the whole batch. |
| `kura_stage` returns success but with `skipped[]` entries (Phase 2) | Per-item probe failed (mediainfo, file vanished mid-flight). The rest of the batch was applied. | Inspect the `code` per skipped row; re-stage the failed items individually. |
| `kura_reconcile_apply` fails as busy | Prior reconcile crashed and left a claim. | Route user to `kura reconcile recover <ref>` (CLI). |
| Series shows `error` in `kura_list` | Library couldn't read the series. | `kura_show(ref)` surfaces the reason; usually requires user intervention. |
| `kura_list` returns `server_not_ready` | Index is rebuilding (cold start, schema mismatch, or corruption recovery). | Wait a few seconds and retry. Don't tight-loop. |
| `kura_list` returns `invalid_cursor` | Cursor corrupt, or anchor row removed in a way that can't resume. | Restart from page 1 (no cursor). |

---

## 11. Talking to the user

- Lead with counts and actionable groupings, not row dumps.
- Use display episode markers (`S01E03`).
- Quote metadata refs verbatim — they're the user's primary handle.
- Don't paste raw `kura_show` JSON. Project to the fields the user
  asked about.
- Prefer the path strings the tools return as-is; don't try to
  absolutize or rewrite them.

---

## 12. Paginating `kura_list`

See the `kura_list` tool description for full pagination rules. Key
rule: loop until `nextCursor` is absent — a final page can be
exactly `maxResults` rows.

---

## 13. What you cannot do via MCP today

- **Inspect or restore trashed files.** CLI only
  (`kura trash list/restore/empty`).
- **Recover a stuck reconcile.** CLI only
  (`kura reconcile recover <ref>`).
- **Untrack or delete a series.** CLI only (`kura remove`).
- **Cross-series moves or merges.** Not modeled.
- **Reach files outside `KURA_INBOX_ROOT` or a series root.**
  Selectors gate every path-bearing input. To stage a file the agent
  has access to but kura doesn't, the user moves it into the inbox
  first.

Route the user to the CLI when these come up.

---

## 14. Selector cheat sheet

See the `kura_stage` tool description for the full selector table
and scheme rules.
