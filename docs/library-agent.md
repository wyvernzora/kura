# Kura Library Management — Agent Guide

You manage an anime/TV library through Kura's MCP tools. This document tells you what each tool does to the library, when to use it, and how to chain them.

---

## 1. What the library contains

A **library** is a flat collection of **series**. Each series:
- has a stable **metadata ref** like `tvdb:370070` (the primary handle for every tool except `kura_resolve`),
- carries provider info (titles, air dates, season/episode list),
- has zero or more **episode slots** — one per episode the provider knows about,
- and may have a **media file** installed in each slot.

A series is either **tracked** (Kura is managing it) or **untracked** (a directory exists but Kura was never told what it is).

**Episode slot states the agent cares about:**
- `present` — file installed and reachable.
- `staged` — a file is *queued* to be installed at this slot, awaiting reconcile.
- `staged_replacement` — staged file will replace the currently-present one.
- `missing` — aired episode, no file.
- `pending` — episode hasn't aired yet.
- `unavailable` — record exists but file is gone from disk.

**Series-level rollups (`kura_list`):**
- `complete` — every aired episode is present.
- `incomplete` — at least one aired episode is missing.
- `airing` — current episodes present, future ones still pending.
- `untracked` — directory not registered with Kura.
- `error` — couldn't read the series.

Episode markers use the form `S01E03`. Both `S01E03` and `S01E0003` are accepted as input.

---

## 2. Tool catalog

| Tool | Effect on library |
|---|---|
| `kura_resolve` | None. Looks up metadata refs from titles. |
| `kura_list` | None. Returns all series with status. |
| `kura_show` | None. Returns full state of one series. |
| `kura_add` | Creates a new tracked series. Empty directory. |
| `kura_import` | Marks an existing untracked directory as tracked. |
| `kura_stage` | Queues "install file X at slot Y". File not moved. |
| `kura_reset` | Cancels queued stages. No file change. |
| `kura_reconcile_plan` | None visible. Computes what `reconcile_apply` would do. |
| `kura_reconcile_apply` | Moves staged files into place. Displaces old files to trash. **Async.** |
| `kura_scan` | Re-reads the series's directory and updates which file is recorded for each slot. **Async.** |
| `kura_job_status` | None. Polls an async job. |

**Async tools** return a `jobId` immediately. Poll `kura_job_status` until the job's `state` is terminal (`succeeded`, `failed`, or `cancelled`). Don't assume a job succeeded without polling.

---

## 3. Operating rules

1. **Default to autonomy.** When you encounter an issue (duplicate slots, missing source token, wrong-subdir file, mis-numbered episode), try to fix it using the available tools. Only escalate when you cannot articulate a clear reason for the choice you would make.
2. **Always have an articulable reason.** Before mutating anything, you must be able to state in one sentence why this action is the right one. "BluRay 1080p beats WebRip 720p" is a reason. "It feels right" is not. No reason → surface to user.
3. **Be consistent.** When the same kind of choice recurs (e.g. multiple duplicate slots across the same series, multiple release groups providing copies), apply the same criterion across the whole set. Don't pick Group A for E01 and Group B for E02 unless there's an explicit per-slot reason.
4. **Always get a metadata ref from `kura_resolve` first** unless you already have one from `kura_list` / `kura_show`. Never invent or guess refs.
5. **Tools that take `ref` accept exactly one ref**, not a title.
6. **`kura_resolve` cardinality drives behavior:** 0 → no match, surface to user. 1 → safe to act. 2+ → disambiguate before acting (see §5).
7. **After `kura_scan` or `kura_reconcile_apply`**, poll the job until terminal. Treat `failed` like any other tool error.
8. **Ask before mutating** only when rules 1–3 don't yield a clear answer. Adding, importing, staging, reconciling, and resetting all change the library. Resolve / list / show / job_status are read-only and free to call.
9. **Don't echo the user's inputs as new info.** Tool responses already drop fields the caller passed in. Surface what's *new*: discovered files, statuses, conflicts, decisions you made.

---

## 4. The five recurring workflows

### 4.a Triage

```
kura_list
```

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

Inspect the scan result for `synced` (slots populated) and `skipped` (files Kura couldn't place — see §6).

### 4.c Add a new series the user is acquiring

```
kura_resolve(terms=[<title>])
kura_add(ref=<chosen>)         # creates the directory
# user drops files in over time
kura_scan(ref=<chosen>) → jobId
kura_job_status(jobId)
```

### 4.d Install a file at a specific slot

Stage takes any file Kura can locate. `mediaPath` and `companionPaths` accept either absolute paths or series-root-relative slash paths — the same form `kura_scan` and `kura_show` emit, so paths from those tools can be passed back verbatim without computing absolute paths.

Use cases:

- **From outside the series directory** (inbox, downloads): file gets moved into the canonical location on reconcile.
- **From inside the series directory** but in the wrong place: stage moves/renames it into canonical layout (e.g. flat dir → `Season N/`, or fixes a wrong filename).
- **Filename scan couldn't infer** (cryptic name, non-standard numbering): stage explicitly assigns it to a slot.
- **Picking a winner among duplicate-slot files** (see §7): stage the chosen file to register it as the active record for that slot.

```
kura_stage(
  ref=<series>,
  episode="S01E03",
  mediaPath="Season 1/foo.mkv",        # absolute or series-root-relative
  companionPaths=["Season 1/foo.en.ass"],  # subtitles, art — see below
  source="BluRay",                     # see below
)
kura_reconcile_plan(ref=<series>) → {token, plan}
# show plan to user if user-driven; get confirmation
kura_reconcile_apply(ref=<series>, token=<token>) → jobId
kura_job_status(jobId)
kura_scan(ref=<series>) → jobId      # adopt the moved file
kura_job_status(jobId)
```

**Always set `source` on best effort.** Parse the filename for tokens (`BluRay`, `BDRip`, `WebRip`, `Web-DL`, `HDTV`, `TVRip`, `DVDRip`). If the filename is silent, look at sibling files in the same release for a hint. If sources genuinely can't be inferred, omit it — Kura will record `Unknown` and you can correct later. Don't guess wildly; a wrong source is harder to spot than `Unknown`.

**Always discover companions.** Look in the same directory as `mediaPath` for sidecar files matching the media basename — subtitles (`.ass`, `.srt`, `.vtt`, `.ssa`), chapters (`.txt`), cover art (`.jpg`, `.png`), thumbnails, NFO files. Pass them all in `companionPaths` so they travel with the media on reconcile. Missing companions on stage means they get orphaned when the media moves.

If a file is already at the slot, the staged file replaces it; the prior file goes to trash. Plans expire 5 minutes after creation; re-plan if you wait longer.

To abort before reconcile: `kura_reset(ref, episode=...)`.

### 4.e Refresh after the user changed the directory

User added, removed, or renamed files; or wants updated provider info applied.

```
kura_scan(ref=<series>) → jobId
kura_job_status(jobId)
```

Examine `synced` rows (statuses `added` / `updated` / `unchanged` / `removed` / `replaced`) and `skipped`.

---

## 5. Disambiguating `kura_resolve`

When `candidates` returns 2+ entries, use structured fields before asking the user.

| Field | Use it for |
|---|---|
| `genres` | `Animation` / `Anime` → animated adaptation; absence → usually live-action. |
| `originalLanguage` | `ja` for Japanese productions, etc. |
| `originalCountry` | `JP`, `KR`, `US`, etc. |
| `year`, `firstAired` | Distinguish sequels, remakes, spinoffs. |
| `evidence` | Per-term match info; useful when titles overlap. |

Heuristics:
- Anime-first libraries: when a title matches both an animated adaptation and a live-action one, prefer the animated candidate.
- Multiple year-tagged candidates with the same title (`Foo (2019)` vs `Foo (2020)`) usually mean season 2 / remake / spinoff. Confirm with the user.
- If structured fields don't decide it, surface candidates verbatim and ask.

---

## 6. Reading scan results

`synced[]` — one entry per slot the scan touched:

| status | meaning |
|---|---|
| `added` | New file installed at a slot that was empty. |
| `updated` | Same file at the same slot, contents changed. |
| `replaced` | File at this slot replaced by a different file. |
| `unchanged` | No change. |
| `removed` | Previously-recorded file is gone from disk. |

`skipped[]` — files the scan declined to place:

| code | meaning |
|---|---|
| `special_number_not_inferred` | File in specials directory, no `S00Exx` token in name. |
| `episode_number_not_inferred` | No episode number in name. |
| `season_mismatch` | Filename season disagrees with directory season. |
| `ignored_directory` | Excluded subdirectory (e.g. extras). |
| `duplicate_slot` | Multiple files claim the same slot. See §7. |
| `metadata_slot_missing` | Filename parses to a slot the provider doesn't have (e.g. file says `S01E25` but season 1 is only 24 episodes). |

`orphanSlots[]` — slots the library still tracks but the provider no longer lists. Informational; usually a metadata revision.

---

## 7. Duplicate slots

When two files claim the same `(season, episode)`, scan reports both as `skipped` with `duplicate_slot` and **leaves the slot empty**. Kura does not pick a winner — that's your job.

Per-slot ranking (highest first):
1. **Source.** BluRay > WebRip ≈ Web-DL > HDTV ≈ TVRip ≈ DVDRip > Unknown.
2. **Resolution.** More pixels wins (1080p > 720p > 480p).
3. **File size.** Same source + resolution → larger usually means higher bitrate. Treat ties within ~10% as "no preference".

**When duplicates span the series** (multiple release groups have provided full or partial sets), pick at the *release-group level* before going slot by slot:

1. Identify the distinct release groups / sources present (filename tokens, source folders, naming conventions).
2. Pick the group whose set is **most complete** at the **highest quality**. A full BluRay 1080p run beats a half-finished BluRay 1080p remux paired with WebRip fillers.
3. Apply that group's files to every duplicate slot. Don't mix.
4. Only fall back to per-slot ranking when the chosen group is missing an episode the alternate group has.

Stage each chosen file (see §4.d) — set `source` from the filename, pull in companions. Articulate the choice in your response: "Picked the [Group X BluRay 1080p] release across S01; falls back to [Group Y WebRip 1080p] for E07 which Group X is missing."

Loser files remain in the directory unreferenced; future scans keep flagging them as `duplicate_slot` skips. Either remove them (filesystem tools or route to user) or accept the recurring skip noise — active records still point at the winners.

Escalate to the user only when:
- Rankings genuinely tie (same source, same resolution, similar size) and you can't articulate a tiebreaker, or
- The files are something you can't classify (no source/resolution tokens, opaque filenames), or
- The release groups are visibly different in cohesion / quality but neither has an obvious advantage (e.g. one is older but stable, one is newer but partial).

---

## 8. Stage vs scan — picking the right tool

- **Scan** is for files *already in the series directory*, named conventionally. Bulk discovery and refresh.
- **Stage + reconcile** is for any case scan can't (or shouldn't) handle on its own:
  - File lives outside the series directory (inbox, downloads).
  - File is in the series directory but in the wrong subdirectory or with a non-canonical name (stage moves/renames it on reconcile).
  - Filename can't be auto-inferred to a slot (cryptic name, non-standard numbering).
  - One of several duplicate-slot files needs to be picked.
- Don't stage a file that's already in place and well-named — scan handles that.
- Don't expect scan to pull in files from outside the series directory — it doesn't walk those.

---

## 9. Reconcile plan and apply

`kura_reconcile_plan` is a preview. It returns a `token` and the list of moves that would happen. Nothing changes on disk. Show this to the user when intent matters.

`kura_reconcile_apply` consumes the token, performs the moves, and updates records. Returns a `jobId`. While running, other mutating tools on the same series may be blocked — wait for terminal state.

If `reconcile_apply` fails mid-flight and a follow-up call complains the series is busy, the user needs to clear the stale state via the CLI (`kura reconcile recover <ref>`) — this is not exposed through MCP. Surface it.

---

## 10. Common failures

| Symptom | Likely cause | Action |
|---|---|---|
| `kura_resolve` returns 0 | Title romanization mismatch or provider gap. | Try alternate titles; ask the user for an explicit ref if they have one. |
| `kura_scan` skips good-looking files with `metadata_slot_missing` | Provider numbers seasons differently than the user's filenames. | Inspect with `kura_show`; user may need to renumber files or stage explicitly. |
| Source column shows resolution string (e.g. `1920x1080`) | Filename suffix has only resolution, no source token. Records show `Unknown` correctly on fresh scans of recent binaries. | Re-scan; if still wrong, override via `kura_stage(source=...)`. |
| `kura_stage` errors with episode-already-exists | Slot already has a recorded file at a different path. | Confirm with user, then re-call with `replace=true`. |
| `kura_reconcile_apply` fails as busy | Prior reconcile crashed and left a claim. | Route user to `kura reconcile recover <ref>` (CLI). |
| Series shows `error` in `kura_list` | Library couldn't read the series. | `kura_show(ref)` surfaces the reason; usually requires user intervention. |

---

## 11. Talking to the user

- Lead with counts and actionable groupings, not row dumps.
- Use display episode markers (`S01E03`).
- Quote metadata refs verbatim — they're the user's primary handle.
- Don't paste raw `kura_show` JSON. Project to the fields the user asked about.
- Prefer the path strings the tools return as-is; don't try to absolutize or rewrite them.

---

## 12. What you cannot do via MCP today

- **Inspect or restore trashed files.** CLI only (`kura trash list/restore/empty`).
- **Recover a stuck reconcile.** CLI only (`kura reconcile recover <ref>`).
- **Untrack or delete a series.** CLI only (`kura remove`).
- **Cross-series moves or merges.** Not modeled.

Route the user to the CLI when these come up.
