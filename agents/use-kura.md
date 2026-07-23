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

- `complete` — every actionable aired episode is present; pending-only series are complete.
- `incomplete` — at least one aired episode is missing.
- `isAiring` — independent flag for a currently airing cour.
- `untracked` — directory not registered with Kura.
- `error` — couldn't read the series.

Episode markers use the form `S01E03`. Both `S01E03` and `S01E0003`
are accepted as input.

---

## 2. Tool catalog

| Tool | Effect on library |
|---|---|
| `kura_resolve` | None. Looks up metadata refs from titles. |
| `kura_aliases` | None. Returns provider titles plus user aliases for a tracked series. |
| `kura_list` | None. Returns one page of series with summary state. Paginated — default 100 rows/page, max 1000. See §12. |
| `kura_show` | None. Returns full state of one series. |
| `kura_inbox_list` | None. Lists files under `KURA_INBOX_ROOT` so you can discover what's available to stage. Structured output, mtime-desc sorted. See §4.d + §13. |
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
10. **Never string-match library rows against external release
    titles.** This includes inbox filenames (`kura_inbox_list`),
    raw release titles from RSS feeds, web-search results,
    tracker listings, IRC announce, manual user-pasted strings —
    anything that isn't already a MetadataRef. `kura_list` returns
    library titles (preferred / canonical) and SeriesRefs; the
    external sources carry release-group brackets, romanization
    variants, multi-language title segments, source / quality
    tags, and date stamps. Direct or fuzzy intersection misses
    real matches and produces phantom ones. The provider is the
    authoritative resolver: every release candidate goes through
    `kura_resolve` to obtain a MetadataRef, then `kura_show` to
    read library state. See §4.g.
11. **User references to inbox locations are `inbox:` selectors.**
    When the user says "look at inbox/pending", "the bdrip dir
    under inbox", "inbox/foo/bar", "the pending folder", etc.,
    they mean the path under `KURA_INBOX_ROOT` — i.e. the
    `inbox:<rel>` selector format kura tools already accept. Pass
    the relative path to `kura_inbox_list(path=<rel>)` to scope a
    listing to that subdir, and use `inbox:<rel>/...` selectors
    when staging files from there. Never absolutize, never strip
    the prefix to a bare path; the inbox root is operator
    configuration kura owns.

---

## 4. The recurring workflows

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
4. `complete` — leave alone unless user wants upgrades.

Use `isAiring` as informational context inside those groups.

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

**CJK / multi-byte paths.** Always copy `inbox:` selector paths
byte-for-byte from `kura_inbox_list` output. Never paraphrase,
re-romanize, or re-encode through a Unicode escape representation
on the way to a stage call. NFC vs NFD, kana / latin substitution,
half-width / full-width forms — any of these produce a path that
looks visually identical but fails to open with `not_found` at
stage time. The fix is mechanical (re-list, re-copy); the symptom
("file vanished") wastes a turn either way.

**Source when files lack tags.** `kura_stage` runs mediainfo but
mediainfo can't infer source (BluRay, WebRip, …) from container
metadata alone — it needs a filename token. When stage records
`source: "Unknown"` and the user hasn't passed an explicit
override, surface it before reconciling. Default action is **ask**,
not guess. Suggest a value only when the filename or a sibling in
the same release directory carries an unambiguous source token
(`BD`, `BDRip`, `BluRay`, `WebRip`, `WEB-DL`, `HDTV`); never infer
from codec / bitrate / size alone — those overlap heavily across
sources. Apply only what the user confirms; if the user says
proceed as-is, keep `Unknown`.

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

### 4.e Bulk adoption of multiple independent series

Operations across *different* series share no state and parallelize
freely. Operations within a single series share the per-series CAS
claim and must run in order. Use that to cut latency when
onboarding N series at once:

```
# Resolve every title in parallel — one MetadataRef per series.
kura_resolve(terms=[<title A>])    kura_resolve(terms=[<title B>])   ...

# For each series, decide add vs already-tracked. Run in parallel.
# Prefer kura_show first when uncertain — kura_add errors if the
# series exists, and a not_found from kura_show is the cheapest
# discriminator.
kura_show(ref=<A>) || kura_add(ref=<A>)    kura_show(ref=<B>) || kura_add(ref=<B>)   ...

# Stage every series in parallel; each is its own batch + its own
# job. Poll all jobIds until terminal before continuing.
kura_stage(ref=<A>, episodes=[...])    kura_stage(ref=<B>, episodes=[...])   ...

# Plan every series in parallel. Tokens are series-scoped.
kura_reconcile_plan(ref=<A>)    kura_reconcile_plan(ref=<B>)   ...

# Apply every series in parallel. Each apply is its own job; poll
# all until terminal. A failure in one series doesn't roll back
# the others.
kura_reconcile_apply(ref=<A>, token=<tokenA>)    kura_reconcile_apply(ref=<B>, token=<tokenB>)   ...
```

Within a single series the order is fixed: stage → plan → apply.
Plan tokens are a hash of the series snapshot, so any state change
between plan and apply (extra stage, scan, manual surgery) flips
the snapshot and apply rejects with a snapshot-mismatch error;
re-plan to get the new token.

### 4.f Refresh after the user changed the directory

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

### 4.g Release triage / adoption

Goal: figure out whether each release candidate should be adopted
into the library, discarded, or used to upgrade what's already
there. Candidates come from one of:

- **Inbox items** — `kura_inbox_list` filenames the user has
  already downloaded. Scope the listing to a subdir when the user
  narrows the request (e.g. "inbox/pending" → `kura_inbox_list(path="pending")`,
  per rule §3.11).
- **External release feeds** — RSS, tracker listings, IRC
  announce, web-search results, manual user-pasted titles.
  Anything that arrives as a raw release string from outside
  kura.

Same problem in both cases: the candidate string is opaque
release-formatted text (release-group brackets, romanization
variants, source tags, multi-language title segments,
date-stamped batches), and the library is keyed by provider
title + SeriesRef. The intuitive shortcut — list the library,
list the candidates, compare names — does not work. String
matching produces both false negatives (real matches missed
because romanization differs) and false positives (unrelated
shows that share a token). The provider is the only authority
that can resolve "what show is this release for" reliably.

**Per-candidate workflow.** For each release candidate (or
small cluster that obviously shares a series), run a fresh
resolution loop. The orchestration is identical whether the
candidate is an inbox file or an RSS title, and identical whether
you're driving it from a single context or fanning out to
subagents:

```
# Step 1: feed the raw candidate string (or a salient title
# fragment) to the resolver.
kura_resolve(terms=[<release title or inbox name or fragment>])
# 0 candidates → can't identify; surface to user.
# 1 candidate  → safe MetadataRef.
# 2+ candidates → disambiguate per §5 before continuing.

# Step 2: read library state for the identified series.
kura_show(ref=<chosen metadata ref>)
# Returns: tracked? episode slots? what's already present /
# staged / missing? lastScanned freshness?

# Step 3: form an adoption recommendation from the show output:
# - Untracked or no overlap → candidate to adopt (kura_add or kura_import).
# - Overlapping slots already present at equal or better quality →
#   candidate to discard.
# - Overlapping slots present at worse quality → candidate to
#   stage with replace: true (per §5 ranking).
# - Missing slots that this release fills → candidate to stage.
```

**Subagent fan-out.** If the harness supports subagents, spawn one per
candidate (or per small cluster) and have it execute steps 1–3 in
isolation, then return just the recommendation summary. Benefits:

- Isolated context per candidate keeps the main agent's window
  small even with dozens of releases under consideration.
- Each subagent's resolve / show output stays scoped to its own
  candidate; no risk of cross-contamination between unrelated
  shows.
- Failures are local — a failed resolve on one candidate doesn't
  pollute reasoning about the rest.

The main agent aggregates subagent reports into the final
adopt-vs-discard table for the user.

**Without subagents** (single-context harness), process candidates
sequentially: resolve + show one, write the recommendation, move
on. Resist the urge to bulk-resolve: the per-candidate evidence
needs to drive the per-candidate decision.

**Directory releases — adopt everything.** When the candidate is a
directory (BD batch, season pack, multi-disc rip), the default is
**adopt every file in it**, not just the episode media. Sort the
contents into the right `kura_stage` arrays:

- **Episode media** (canonical-episode files: `S01E03`, `Ep03`,
  numeric, etc.) → `episodes[]` entries with the matching slot.
- **Extras** (NCOPs, NCEDs, PVs, trailers, BTS, interviews,
  bonus episodes, image galleries, anything not part of the
  numbered spine) → `extras[]` entries. Use `prefix:` to give
  them a stable bucket name (`"creditless-openings"`,
  `"behind-the-scenes"`, etc.) so multiple releases don't fight
  for the same `Season N/Extra/` namespace.
- **Companion files** (subtitles, alternate audio, fonts that
  sit next to a specific episode media file) → `companions:` on
  the corresponding episode entry.

If the release directory already contains an **`Extra/` (or
`Extras/`, `Specials/`, `Bonus/`) subdirectory**, treat its entire
contents as the extras input — pass each contained file or
subdirectory through `extras[]` with a sensible `prefix`. Don't
flatten extras into the episode list; they live under
`Season N/Extra/[prefix]/` for a reason. Don't drop them either —
"adopt episodes only" leaves the user manually shoveling extras
back in.

**Classification effort.** Episode mapping is the higher-value
outcome (canonical naming, tracked spine, upgrade detection), so
exhaust the available signals before falling back to extras:

- Filename: `S01E03`, `Ep03`, ` 03 `, `_03_`, `[03]`, `EP03v2`,
  `第3話`, etc.
- Order: file's position in a sorted listing of the directory's
  episode-shaped peers (matches against the spine's known
  episode count).
- File size: episodes within a release are usually within a tight
  band (typically ±20% of the median); extras are most often
  significantly smaller (creditless OPs / EDs, PVs, image
  galleries) and occasionally much larger (uncut interviews,
  full-length BTS). A file whose size is far off the episode
  cluster is almost certainly an extra.
- Sibling tokens: NC, NCED, NCOP, OP, ED, PV, CM, BTS, Special,
  SP, Trailer in the filename are strong "this is an extra"
  signals.

Only fall back to extras when none of those signals identify a
spine slot. `Season N/Extra/[unsorted]/<file>` is the safe
fallback — but treat it as a fallback, not the default. A correct
episode rename is the much more useful outcome; a wrong
canonical-episode rename does leave a trash bucket, but those are
rare when the filename signals are unambiguous.

**What never works.** Computing an intersection between
`kura_list` output and any source of raw release titles
(`kura_inbox_list`, RSS, web-search, tracker JSON, etc.) by
string equality, fuzzy match, prefix overlap, or any other
library-side operation. Anime release naming conventions (CJK ↔
romaji ↔ English, fan-sub brackets, date-stamped batches) defeat
all of those. Always go through the resolver.

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
    with a non-canonical name. Episode media can use a `series:`
    selector only for an in-place metadata override of that same
    episode's active file; relocating an unrelated in-library file
    still means copying it to inbox, staging it, then trashing the
    misplaced original via `series:` selector in the same batch.
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

**Token = snapshot hash.** The token is a 12-char hex prefix of
`SHA256(series.json bytes)`, so it's deterministic on series state.
Same state → same token; any state change (extra stage, reset,
scan, manual surgery) → different token.

**Apply re-validates.** `kura_reconcile_apply` re-reads
`series.json` and recomputes the snapshot before executing. If the
snapshot no longer matches the plan's, apply rejects with a
`stale_snapshot` error. There is no separate TTL: a plan stays
valid as long as the series state hasn't changed.

**Operationally:**

- Plan and apply in the same flight is still the cheap default —
  cuts surprise diagnostics if you mistake what mutated state
  in between.
- If apply returns `stale_snapshot`, call `kura_reconcile_plan`
  again to get the fresh token, then re-apply. Don't try to
  re-use the old token — the snapshot will keep mismatching.
- Long context interruptions (session compaction, user detour) are
  fine **as long as the series state hasn't changed**. Re-plan is
  cheap and idempotent if you want to be safe.

See each tool's description for field semantics, verification, and
busy-recovery guidance.

---

## 10. Common failures

| Symptom | Likely cause | Action |
|---|---|---|
| `kura_resolve` returns 0 | Title romanization mismatch or provider gap. | Try alternate titles; ask the user for an explicit ref if they have one. |
| `kura_scan` skips good-looking files with `metadata_slot_missing` | Provider numbers seasons differently than the user's filenames. | Inspect with `kura_show`; user may need to renumber files or stage explicitly. |
| Source column shows resolution string (e.g. `1920x1080`) | Filename suffix has only resolution, no source token. Records show `Unknown` correctly on fresh scans of recent binaries. | Re-scan; if still wrong, override via `kura_stage(source=...)`. |
| `kura_reconcile_plan` shows the active or staged file as `Unknown` source | Kura's filename parser missed the source token on the originally-staged file (uncommon naming, parser gap). | Check the **original** filename (before kura's rename) for a source token (`BluRay`, `WebRip`, `WEBDL`, `HDTV`, etc.). If present, re-stage the affected file with an explicit source override using a `series:` media selector (in-place override): `kura_stage` episode item with `media: "series:<active-path>"`, `replace: true`, `source: "<token>"`, no companions. Reconcile_apply rewrites the persisted source and renames the file to its corrected canonical filename without moving it through the inbox. |
| `kura_stage` errors with episode-already-exists | Slot already has a recorded file at a different path. | Confirm with user, then re-call with `replace: true` on the episode item. |
| `kura_stage` rejects with "expected inbox: scheme" / "expected series: scheme" | Selector type wrong for the field. Episode `media` accepts `inbox:` normally and `series:` only for in-place metadata override; episode companions and extras `source` need `inbox:` selectors; trash `path` + companions need `series:` selectors. | Re-build the selector with the right scheme. |
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

- **Inspect or restore trashed files.** CLI or REST operator route
  (`kura trash list/restore/empty`).
- **Recover a stuck reconcile.** CLI or REST operator route
  (`kura reconcile recover <ref>`).
- **Untrack or delete a series.** CLI or REST operator route
  (`kura remove`).
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
