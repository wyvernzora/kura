# Library lifecycle

Detailed, technically precise reference for every workflow Kura
supports — adding a new series, scanning, staging files, reconciling
moves, recovering from failure, managing trash, bootstrapping an
existing library, and removing a series.

The project README is the short user-facing entrypoint. This doc is
the engineer-facing version: edge cases, error modes, and operator
surgery.

For the underlying terms (MetadataRef, SeriesRef, EpisodeRef, spine,
claim, mutator, CAS, ULID), see [concepts.md](concepts.md).

## Async jobs

`Scan`, `ApplyReconcile`, `Reindex`, and `ScanAll` are job-shaped: the
server submits them to the registry in `internal/jobs/` and lets the
caller poll. Across surfaces:

- CLI blocks on the job and renders progress inline.
- REST returns `202 Accepted` with `{jobId, statusUrl, streamUrl}`.
  Clients poll `GET /api/v1/jobs/{id}` or stream
  `GET /api/v1/jobs/{id}/stream` (SSE).
- MCP returns a job handle. Agents poll `kura_job_status`.

Per-job forensic logs are written to
`<library>/.kura/jobs/<ulid>.jsonl` and pruned after
`KURA_LOG_RETENTION_DAYS` days (default 7). `KURA_JOB_TIMEOUT` caps
individual job duration; unset means no timeout.

## Workflows

### 1. Add a new empty series

**Intent:** Register a new series in the library before any files
exist for it. Used to express "I want this show but haven't acquired
anything yet" — useful for agents to treat the series as a
hole-filling target.

**Journey:**

1. User or agent resolves a MetadataRef for the series. The CLI does
   this from `add <terms...>`; MCP/REST receive the resolved ref.
2. Caller may supply a directory-name override. Otherwise Kura uses
   the provider preferred title.
3. **Resolved:** Kura creates `<library-root>/<dirname>/`, fetches
   the full spine from the provider, writes initial `series.json`
   containing the resolved MetadataRef and spine, sets `lastScanned`
   to the time of the spine fetch, and updates the index. The series
   is now tracked with no active media records.
4. **Unresolved:** Kura returns candidates. Caller retries with a
   more specific query, typically a MetadataRef.
5. **Not found:** Caller refines the query.

**Edge cases:**

- The supplied SeriesRef already exists under the library root (any
  state — empty, with files, tracked) → error. The user must pick a
  different name, or use `import` if the directory already holds
  files for this show.
- Resolved MetadataRef is already tracked at a different SeriesRef →
  error. The series exists somewhere else in this library.

`add` is a constructive operation: it resolves metadata first, then
creates the directory selected by `--dirname` or the provider title. See
[concepts.md §Selectors, not paths](concepts.md#design-model-internal-invariants).

### 2. Import an existing directory

**Intent:** Bring an existing untracked series directory under Kura
management. Files may follow Kura's naming convention or a previous
one.

**Journey:**

1. Series directory exists under library root with files but no
   `.kura/` content.
2. User or agent calls `import <dirname> [terms...]`. The CLI uses the
   dirname as a text resolution term unless the supplied terms already
   include a MetadataRef; MCP/REST receive `{ref, dirname}` directly.
3. Kura runs series resolution.
4. **Resolved:** Kura fetches the full spine from the provider,
   writes initial `series.json` with the resolved MetadataRef and
   spine, sets `lastScanned`, and updates the index. The series is
   tracked but existing files in the directory are not adopted by
   import.
5. **Unresolved:** Kura returns candidates. Caller retries with a
   MetadataRef term.
6. After import, the caller typically calls `scan <SeriesRef>` to
   adopt existing files into metadata.

**Edge cases:**

- Directory already has any `.kura/` content → error. `import`
  operates on directories without Kura metadata. If `.kura/` exists
  in any state (valid, partial, corrupt), the operator must remove
  it first via surgery (see [Recovery](#recovery-and-surgery)).
- Resolution finds a series already tracked at a different SeriesRef
  → error or warning (caller decides whether to proceed); two
  SeriesRefs sharing a MetadataRef is likely a mistake.

The CLI exposes `--force` to overwrite a corrupted
`.kura/series.json`; MCP and REST do not.

### 3. Scan a tracked series

**Intent:** Re-sync local metadata with current provider and
filesystem reality. Refresh the spine, pick up eligible files, prune
missing media records, refresh mediainfo. Does not move files.

**Journey:**

1. User or agent calls `scan <selector>`.
2. Kura fetches the episode list from the metadata provider. If the
   provider is unreachable, scan fails before touching filesystem
   records.
3. Kura reconciles the spine:
   - Adds new provider slots.
   - Updates air dates for existing slots.
   - Removes slots that no longer exist only when they have no active
     or staged record.
   - Refuses to remove slots with active or staged records and
     surfaces conflicts.
4. Kura walks the series directory.
5. For each file matching the canonical naming convention:
   - If already in metadata with unchanged mtime/size: skip.
   - If already in metadata with changed mtime/size: rerun mediainfo,
     update record.
   - If not in metadata and it claims a slot in the spine: run
     mediainfo, add as active media record.
   - If it claims a slot that is not in the spine: do not adopt it;
     return it as an orphan with a slot-not-in-spine note.
6. For each media record whose file is missing from disk: remove the
   record. (Future query observes Missing or Pending.)
7. For each file in the directory that does not match naming
   convention: returned as orphan in the structured response.
   Companion files are matched to their media file and not flagged as
   orphans.
8. Kura detects companion files: same stem, different extension. Adds
   them to the media record's companion list.
9. Kura updates `lastScanned`.

**Edge cases:**

- Provider call fails (transport error, rate limit) → scan errors out
  before touching filesystem records. Operator retries when network
  is available.
- Series has staged episodes (unreconciled) → scan is disallowed.
  Caller must reconcile or reset first.
- File claims a slot that does not exist in the spine → not adopted;
  surfaced as an orphan with a slot-not-in-spine note.
- Two files claim the same EpisodeRef (different filenames, both
  match the convention) → both returned as orphans; Kura does not
  auto-pick. Caller resolves manually.
- File same mtime/size but different content (in-place replacement
  without touching mtime) → not detected. Explicit accepted
  limitation. Full rehash available via flag.

### 4. Stage a new episode

**Intent:** Add a single new episode file to a tracked series.

**Journey:**

1. New file exists somewhere on disk (typically a download landing
   area).
2. User or agent calls
   `stage episode <selector> <EpisodeRef> <inbox:media> [--companion inbox:path]`.
3. Kura inspects the file: runs mediainfo, records size and mtime.
   Writes the staged record into the slot's `staged` field in
   `series.json`, alongside companion paths. Does not move anything.
4. Caller may stage additional episodes for the same series.
5. User or agent calls `reconcile plan <selector>` to inspect the
   planned moves, then `reconcile apply <selector> <token>` to
   execute them.
6. Kura moves the staged file into its canonical path, moves any
   displaced active file into trash, promotes the slot's `staged`
   record to its `active` record, and appends per-move events to the
   plan's JSONL file under `<series>/.kura/reconcile/<token>.jsonl`.

**Edge cases:**

- An active record already exists at the EpisodeRef at a different
  path, with the file present on disk, and `--replace` was not passed
  → error. Caller must opt in to replacement.
- An active record exists at the EpisodeRef at the same path as the
  staged file → stage proceeds without requiring `--replace`. Active
  record is refreshed in place (mediainfo, source, companions).
  Resulting reconcile plan MUST contain zero file moves for the
  media file itself; only companion-file moves and the metadata
  refresh are emitted. `--replace` is tolerated but never forces a
  media-file move when source and destination paths are equal.
  Planner correctness invariant — has explicit test coverage.
- An active record exists at the EpisodeRef but the underlying media
  file is missing (Unavailable) → stage proceeds without `--replace`.
  Reconcile places the new file with no trash step.
- A staged record already exists at the EpisodeRef and `--replace`
  was not passed → error. Caller must `reset` first or pass
  `--replace`.
- File does not exist at the provided path → error at stage time.
- Canonical destination has a file not recorded in metadata
  (filesystem drift) → reconcile detects, halts, reports. No silent
  overwrite.

### 5. Replace an existing episode

**Intent:** Upgrade an existing active episode with a better version.

**Journey:**

1. Better version available on disk.
2. User or agent calls
   `stage episode <selector> <EpisodeRef> <inbox:media> --replace [--companion inbox:path]`.
3. Kura records the staged record on the slot. The slot now has both
   `active` and `staged` records.
4. User or agent calls `reconcile plan <selector>` then
   `reconcile apply <selector> <token>`.
5. For each staged-replacement: Kura generates a fresh ULID, creates
   `<series>/.kura/trash/<ULID>/`, moves the existing active media
   file and its companions into that ULID directory, writes
   `meta.json` describing the trashing event and the displaced
   record, then moves the staged file and its companions into the
   canonical path. The slot's `staged` record is promoted to
   `active`.

**Edge cases:**

- `--replace` passed but no active record exists → treated as a
  normal stage. `--replace` is tolerated but not required (see
  Workflow 4 edge cases).
- `--replace` passed but the active record's file is missing
  (Unavailable) → reconcile skips the trash step with a warning and
  places the new file. `--replace` is tolerated but not required.
- Staged file is identical to the active file (same hash) → reconcile
  warns; the plan surfaces the warning before any apply.
- Trash move fails (permissions, no space) → reconcile halts before
  placing the new file. Active state preserved.

### 6. Replace a full season

**Intent:** Replace all episodes in a season with better versions —
e.g. a BD batch displacing WEB episode downloads.

**Journey:**

1. Caller stages each episode in the batch with `--replace`.
2. Caller calls `reconcile plan <selector>` then
   `reconcile apply <selector> <token>`.
3. Kura applies all staged records: trashes displaced files, places
   new files. All-or-nothing in intent.

**Edge cases:**

- Batch is incomplete (e.g. 11 of 12 episodes) → caller stages what
  is available. Missing slots retain existing actives.
- Reconcile fails mid-execution → see [Recovery](#recovery-and-surgery).
- Some episodes in the batch had no prior active (were Missing) → no
  trash step for those; they are handled as regular stages within
  the same reconcile.

### 7. Reset staged records

**Intent:** Discard staged intent without touching the filesystem.
Either for a single episode or for all staged records in the series.

**Journey (single episode):**

1. User or agent calls `show <selector>` to inspect staged records.
2. User or agent calls `reset <selector> --episode <EpisodeRef>`.
3. Kura removes the slot's `staged` field from `series.json`. The
   staged file on disk is untouched.
4. If an active record existed beneath a staged-replacement, it
   remains active.

**Journey (all staged records):**

1. User or agent calls `reset <selector> --all`.
2. Kura removes the `staged` field from every slot in `series.json`.
   Staged files on disk are untouched.
3. Active records are unaffected; only staged intent is dropped.

`reset --all` is the verb-level affordance for "abandon all staged
intent for this series" — useful when the operator/agent has changed
direction on a season upgrade, or when staged records have become
stale. It is also part of the standard recovery path when staged
records are logically wedged but parseable (see
[Recovery](#recovery-and-surgery)).

**Edge cases:**

- No staged record at the given EpisodeRef (single-episode reset) →
  error.
- No staged records at all (`--all`) → no-op success. The desired end
  state (no staged records) is already achieved.
- Staged file has been deleted from disk since staging → reset still
  succeeds. Metadata is the concern; the staged file's disk state is
  the caller's responsibility until reconcile.

### 8. Query series state

**Intent:** Observe the current state of a series for an agent or
human to reason about.

**Journey:**

1. User or agent calls `show <selector>`.
2. Kura resolves the selector. Resolution may contact the provider
   for text selectors, but after resolution `show` reads
   `series.json` only — no filesystem access.
3. Kura returns a structured response containing:
   - Series identity (MetadataRef, SeriesRef, `lastScanned`).
   - Every slot in the persisted spine, each with its observed state.
   - For Present/Staged episodes: mediainfo, source, resolution,
     companion files.
   - Outstanding staged records, including replacements.

This is the primary query call. Agents use it to decide whether to
queue a release ("does the show have S01E23? at what quality? is it
missing earlier episodes?"), and humans use it to inspect a show.

There is no targeted episode-level query. Callers fetch the full
series state and pick the episodes they care about. This keeps the
surface area small and gives the agent everything it needs in one
observation.

`show` is read-only with respect to local metadata. After selector
resolution, it does not contact the provider for episode data or rich
series details. If `lastScanned` is stale, `show` still succeeds with
the data it has. Drift between the spine and provider truth is healed
by the next `scan`.

**Edge cases:**

- `lastScanned` is significantly older than current time → `show`
  succeeds with the data it has and surfaces the stale timestamp for
  caller judgment.
- Series has staged records and a previous reconcile failed
  mid-flight → response surfaces both staged records and the
  inconsistency. Caller resolves manually (see
  [Recovery](#recovery-and-surgery)).

### 9. List library inventory

**Intent:** Get a fast, condensed inventory of the library without
performing a full filesystem audit.

**Journey:**

1. User or agent calls `list`.
2. Kura reads the in-memory library index snapshot. If the server has
   not finished its initial rebuild, the call returns a not-ready
   error and the caller retries shortly.
3. For each indexed direct child directory:
   - If `.kura/series.json` is absent, Kura returns an `untracked`
     row. The row title is the directory name suffixed with `*` to
     indicate it is not a provider-backed title.
   - If `.kura/series.json` cannot be read or decoded, Kura returns
     an `error` row.
   - If metadata is valid, Kura reads persisted series title, spine,
     active records, and staged records.
4. Kura returns one row per listed directory with `status`, `title`,
   `metadataRef`, season counts (`seasonsAvailable` / `seasonCount`),
   episode counts (`episodesAvailable` / `episodeCount`), distinct
   resolutions and sources rolled up across active episodes, and
   `lastScanned`. All counts and quality rollups exclude specials.
   Callers may pass repeated `--status` filters to include only
   matching aggregate statuses.

`list` is intentionally index-backed and metadata-only. It does not
stat every recorded media path, so it cannot report `Unavailable`.
Filesystem drift is repaired by `scan`; library directory drift is
picked up by the server's index rebuild/watch path or by `reindex`.

**Series status:**

- `complete` — every currently actionable non-special episode has an
  active or staged media record. A pending-only spine is complete
  because there is no missing media to act on yet.
- `incomplete` — there are zero known non-special episodes, or at
  least one currently actionable non-special episode without
  active/staged media has aired.
- `untracked` — visible direct child directory with no
  `.kura/series.json`.
- `error` — `.kura/series.json` exists but cannot be read or decoded.

`isAiring` is independent of series status. A non-special cour counts
as airing when its first episode has aired or is within the near-start
horizon, and its last episode is no older than the configured
`KURA_AIRING_TAIL_DAYS` window. Split-cour gaps are non-airing until
the next cour nears start.

Specials do not affect status, season count, episode count, or the
staged marker. Staged records satisfy their episode for status
purposes. If any staged record exists on a non-special episode, the
displayed status is suffixed with `*`.

Status filtering compares the underlying aggregate status, not the
display suffix. For example, a series whose missing regular episodes
are all staged is filtered as `complete`, even though the displayed
status is `complete*`.

### 10. Bootstrap an existing library

**Intent:** Adopt an existing on-disk library into Kura tracking. One-
time per library; not an agent steady-state operation.

**Actor:** Operator, via CLI. Agent may assist with resolution.

**Journey:**

1. Operator (or agent) runs `kura list` filtering for `untracked`
   rows. Available on every surface.
2. For each untracked directory to adopt:
   1. Resolve a MetadataRef. Either operator picks one, or agent
      calls `kura_resolve` with the directory name and inspects
      candidates.
   2. Call `import <dirname> <metadataRef>` to adopt the directory
      at the chosen identity.
   3. Call `scan <selector>` to adopt existing files into metadata.
3. After adoption, normal workflows resume.

**Rationale:** `import` establishes durable identity; the cost of a
wrong choice is real, but the choice is fundamentally a `kura_resolve`
decision the agent already makes via candidate inspection. MCP exposes
`kura_import` so an agent processing untracked rows from `kura_list`
can complete the adoption end-to-end. The CLI-only `--force` flag
(overwrite a corrupted `.kura/series.json`) remains the operator-only
escape hatch.

**Edge cases:**

- SeriesRef has no useful signal (romanized abbreviation, generic
  name) → resolution likely Unresolved or Not Found. Operator
  supplies extra terms or picks from candidates.
- Directory contains mixed content not matching any single TVDB
  series → `import` resolves to one show; subsequent `scan` returns
  the unrelated files as orphans. Operator handles them out of band.

### 11. Remove a series

**Intent:** Stop tracking a series. Infrequent operator action.

**Journey:**

1. Operator calls `kura remove <selector>` from the CLI.
2. Default behavior: Kura deletes the series's `.kura/` metadata
   directory and removes its entries from the library index. Media
   files are left in place. The directory becomes untracked.
3. With `--purge --confirm`: Kura removes the entire series directory
   (media, metadata, trash, all). `--confirm` is required.

`remove` is intentionally CLI-only and not exposed via MCP. REST
exposes it only when both `X-Kura-Operator: 1` and (for `--purge`)
`X-Confirm: 1` headers are present. Untracking and deletion of media
should not happen at agent or browser-driven initiative; it stays in
operator hands.

**Edge cases:**

- Selector resolves to a series with staged records → default
  `remove` errors. Caller must `reconcile` or `reset --all` first.
  `--purge` bypasses the gate (wholesale delete drops the staged
  records along with everything else).
- Selector resolves to a series with non-empty trash → default
  `remove` leaves the trash on disk (since the directory is left
  behind); `--purge` deletes it with the rest.
- Selector does not resolve to a tracked series → error.

## Trash management

Each series has its own trash directory at `<series>/.kura/trash/`.
Trash is per-series so it travels with the series — moving a series
directory to another library carries its trash too. Each trashing
event gets a fresh ULID directory: `<series>/.kura/trash/<ULID>/`.
That directory contains the displaced media file, its companions, and
`meta.json` describing the trashing event, slot, timestamp, and full
displaced media record. Presence of `meta.json` asserts that the
trashing event completed.

**Journey:**

1. Operator calls `trash list <selector>` for one series, or
   `trash list --all` for the entire library (full walk).
   `--older-than DURATION` filters by age.
2. Kura returns one entry per ULID directory, reading `meta.json`
   from each: ULID, slot, trashed-at timestamp, media file,
   companions, and full media record. Per-series rollup with totals.
3. Operator calls `trash empty <selector>` for one series, or
   `trash empty --all --confirm` for the entire library.
4. Kura permanently deletes trashed files via `os.RemoveAll` per ULID
   dir. Reclaimed size is reported.
5. To recover a trashed entry instead of deleting, operator calls
   `trash restore <selector> <ULID>`. Kura moves the media file and
   its companions back to the recorded paths and removes the
   now-empty trash dir. Operator runs `scan` afterward to re-adopt
   the files into `series.json`.

**Edge cases:**

- `trash empty --all` without `--confirm` → errors. Library-wide
  deletion is opt-in.
- `trash list` / `trash empty` without a selector AND without
  `--all` → errors. Implicit library-wide invocation is intentionally
  not supported.
- `trash restore` target paths already exist → errors with the
  conflict list. Operator must move the existing files out of the
  way before retrying.
- A trashed file referenced by `meta.json` has been deleted outside
  Kura → empty silently skips it; restore errors at move time.
- Manually restoring a file from trash uses `meta.json` to identify
  the original slot and media record. The `scan` re-adoption step
  handles `series.json` reconciliation.

## Recovery and surgery

A reconcile is all-or-nothing in intent. The filesystem operations
that implement it are sequenced — file moves, trash operations,
metadata writes — and if the process is killed or hits an error
mid-execution, some operations have happened and others have not.
Kura does not automatically resume or rollback; the operator/agent
resolves manually.

A JSONL file is written for every reconcile plan at
`<series>/.kura/reconcile/<token>.jsonl`. Line 1 is the immutable
plan header; lines 2..N are the planned steps; subsequent lines are
append-only events written by `reconcile apply` (one per move
attempt plus a terminal `result`). The log is forensic: it makes
"what state is the series in after this failure?" answerable by
inspection rather than guesswork. Each completed trashing event also
leaves a `meta.json` inside its ULID directory under
`<series>/.kura/trash/`, providing self-describing forensic context
independent of the plan log.

**Recovery journey for a failed reconcile:**

1. Reconcile fails or is interrupted. Kura returns an error
   indicating which step failed.
2. User or agent calls `show <selector>`. The response surfaces
   inconsistencies between staged intent, recorded metadata, and
   observed filesystem state.
3. Caller chooses one of:
   - **Continue forward.** Stage and reconcile the remaining changes
     individually, working with whatever partial state exists.
   - **Surgery + scan.** If the series's metadata is wedged badly,
     perform recovery surgery (matrix below) to bring metadata back
     in line with filesystem.
   - **Manual cleanup + scan.** Move files around manually outside
     Kura to reach a known-good filesystem state, then `scan` to
     resync metadata.

There is no `reconcile rollback` or `reconcile resume` operation. The
recovery affordances are the standard verbs (`stage`, `reconcile`,
`scan`) plus operator surgery on `.kura/`.

**Stuck-claim recovery:** if `reconcile apply` was interrupted
mid-flight, the in-progress claim recorded in `series.json` may
remain. `kura reconcile recover <selector>` clears it. By default the
recover verb only breaks claims whose holder process is no longer
running on the same host; `--force` breaks any claim unconditionally.
On `kura serve` startup, a one-shot sweep clears same-host stale
claims automatically (see [deployment.md](deployment.md)).

### Recovery matrix

Filesystem is truth, and `.kura/` is a regenerable projection of
(filesystem state + provider knowledge + recorded user intent). Any
wedged or corrupt local state can be recovered by deleting the
offending files and re-running the appropriate verb. This is a
first-class affordance, not a fallback — every layer of the system
is designed assuming this escape hatch exists.

| Situation | Recovery |
|---|---|
| `.kura/` totally gone or `series.json` missing/corrupt | Operator removes any partial `.kura/series.json` content. Then `import <dirname> [terms...]` to re-establish identity and spine. Then `scan <selector>` to adopt files. Trash and logs are preserved through this flow. |
| Staged records reference stale or unreachable files but `series.json` is parseable | `reset <selector> --all`, then `scan <selector>`. |
| `series.json` is parseable but logically wedged in some other way | Operator removes `series.json`. Then `import <dirname> [terms...]` and `scan <selector>` to rebuild from filesystem + provider. |
| Wedged plan log files | Operator removes the offending JSONL files in `<series>/.kura/reconcile/`. They are append-only forensic; no verb consults them outside `reconcile apply`'s own token lookup. |
| Spine is significantly out of date relative to current TVDB state | `scan <selector>` to refresh. |
| A reconcile failed mid-execution and the series is in an inconsistent state | Standard verbs (`stage`, `reconcile`, `scan`) plus targeted surgery resolve it. Inspect the plan JSONL or trash `meta.json` for what already happened. |
| Reconcile placed an active file in trash unexpectedly (planner bug or staged-intent mistake) | (1) `kura trash list <selector>` to identify the trashed file. (2) `kura trash restore <selector> <ULID>` to put it back at its original path. (3) `kura reset --all <selector>` to clear staged intent that may re-trigger the same plan on next reconcile. (4) Optionally `kura scan` to refresh metadata. |
| In-progress claim from crashed apply blocks new operations | `kura reconcile recover <selector>` (adds `--force` if cross-host). |

### Principles

- **Identity and trash are the local state worth preserving.** The
  MetadataRef in `series.json` is what the user/agent painstakingly
  resolved; trash is content preservation. The spine and media
  records are re-derivable from filesystem + provider call.
- **Surgery is composing `rm` with Kura verbs.** No special
  recovery-mode operations. The operator removes broken files; the
  standard verbs handle the rest.
- **Trash is never collateral damage.** The recovery matrix never
  deletes trash. Trash is operator-managed via `trash empty` only.
  The sole exception is `remove --purge`, which deletes the entire
  series wholesale.
- **`import` operates on directories without Kura metadata.** If
  `.kura/` is present in any state — valid, partial, corrupt —
  `import` errors. Operator surgery comes first.

### What surgery cannot do

Surgery cannot recover content that has been permanently deleted. If
`trash empty` has been run and the operator subsequently realizes a
trashed file should have been kept, that file is gone. This is the
trade-off for explicit deletion: the operator's confirmation is the
durability boundary.

Surgery also cannot recover the user's *intent* about a partial
reconcile. If a reconcile failed mid-execution and the operator
resolves it via `scan`, Kura no longer knows what the operator
originally meant to do — only what the filesystem currently looks
like. The reconcile log file is the forensic record; it is not a
basis for automatic continuation.
