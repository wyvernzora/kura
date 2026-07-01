# Concepts

Engineer-facing reference for Kura's data model, vocabulary, and the
invariants the codebase upholds. The README's glossary covers the
user-facing nouns; this doc is for everything underneath.

## Actors

**User** — the human owner of the library. Interacts directly via CLI,
indirectly via an AI agent, or via the bundled web dashboard.

**Agent** — an AI agent (Claude, Cursor, custom) that consumes Kura's
MCP tool surface. Acts on behalf of the user. May operate autonomously
or with user confirmation for ambiguous cases.

**Operator** — the human running the deployment. The only actor allowed
to invoke permanently-destructive verbs (`trash empty`, `remove --purge`).
Operator-only operations are CLI-only or REST endpoints gated by an
explicit operator header (`X-Kura-Operator: 1`).

## Vocabulary

A small set of named identifiers shows up across operations and
surfaces. Standardizing on them avoids term drift.

**Provider** — an external metadata source. Currently TVDB; the system
is built to allow alternatives (AniList, MAL, AniDB) without a redesign.

**MetadataRef** — the identity of a series as defined by a provider.
Format: `<provider>:<id>`, e.g. `tvdb:370070`. Stored in `series.json`.
Every tracked series has exactly one. Authoritative for "which show is
this?".

**SeriesRef** — local reference to a series. Effectively the name of
the series's directory under the library root. The on-disk anchor for a
tracked series; what you would type into `cd`. The library index maps
MetadataRefs to SeriesRefs.

**Selector** — the umbrella concept of "anything you can pass to
identify a series in an operation." A selector resolves (via the
resolver) to a MetadataRef, which the caller can then act on or which
Kura uses internally to find the SeriesRef via the index.

**EpisodeRef** — local reference to an episode slot. Format:
`S<NN>E<NNNN>`, e.g. `S01E0001`. Used as keys in `series.json` and as
parameters to operations. Fixed-width so lexicographic key order matches
natural episode order.

**Spine** — the persisted set of episode slots a series has. Each entry
holds slot identity (EpisodeRef) and air date. Local data attaches to
spine entries.

**Holder** — identity tuple recorded on an in-progress claim:
`{op, token, pid, host, started}`. Identifies who holds the claim
(operation, process PID, hostname, start time).

**Mutator** — identity tuple stamped on every metadata write:
`{op, pid, host, at}`. Records who last wrote the file.

**CAS (compare-and-swap)** — write semantics for `series.json`. The
writer reads a hash with the file, mutates in memory, then writes only
if the on-disk hash still matches. On conflict, the writer reloads and
retries. `index.jsonl` is serialized by the process index coordinator
and is rebuildable from per-series metadata.

**ULID** — universally unique lexicographically sortable identifier.
Used for trash entry directories and job IDs.

## Domain model

### Library

A root directory containing one or more series folders. Each series
folder is a direct child of the library root. Kura does not support
nested or non-standard layouts. The library layout is generally
compatible with common media servers like Plex and Jellyfin.

The library root contains one library-wide artifact:
`.kura/index.jsonl`, a regenerable source snapshot for MetadataRef to
SeriesRef lookup plus fast `list` projections. The index is not
authoritative — it can be deleted and rebuilt at any time from
per-series metadata via `kura reindex`.

### Series

A single anime show, made of seasons and episodes. Represented on disk
as a direct child directory of the library root. Contains:

- Media files for episodes, named per the canonical naming convention.
- Companion files (subtitles, alternate audio) named to match their
  episode.
- A `.kura/` metadata directory holding `series.json` (the single
  per-series metadata file), a trash directory at `.kura/trash/`, and
  per-plan reconcile JSONL files under `.kura/reconcile/`.

Series and seasons are organizational buckets. Neither has its own
state — only episodes do, and even episode state is observed, not
stored.

### Episode

The atomic unit of tracked content. Each episode is a slot in the
persisted spine, identified by an EpisodeRef. The spine is populated
from the provider on add/import and refreshed on scan. A slot may carry
at most one active media record and at most one staged media record.

Multi-episode files are not supported. One media file per episode,
period. Releases packaging multiple episodes per file must be split
before staging.

### Companion files

Files that accompany a media file but are not the media themselves:
external subtitles (`.srt`, `.ass`), alternate audio tracks, fonts.
Companion files are named identically to their associated media file
with a different extension (and optional language tag), and are tracked
alongside the media record they accompany. Companions follow their
media record through replacement and trash operations; they are never
tracked or moved independently.

## Episode state (observed)

Episode state is **derived at query time** from persisted metadata:

1. Spine entry in `series.json` — does Kura know the slot and its air
   date?
2. Media records in `series.json` — is there an active or staged record
   for this slot?

The five observable states are derived as follows:

| Slot record | Air date | Active record | Staged record | Observed state                   |
|-------------|----------|---------------|---------------|----------------------------------|
| Present     | future   | absent        | absent        | **Pending**                      |
| Present     | aired    | absent        | absent        | **Missing**                      |
| Present     | any      | present       | absent        | **Present**                      |
| Present     | any      | absent        | present       | **Staged**                       |
| Present     | any      | present       | present       | **Staged with pending replacement** |

`show` reads `series.json` only — it does not probe the filesystem.
Whether a recorded file is still physically present on disk is not
checked at query time. `scan` is the mechanism for reconciling
filesystem reality back to metadata; run it when in-flight moves or
external changes may have left the metadata stale.

Provider state is not consulted at query time. State is never
persisted. A file claiming a slot outside the spine is not adopted.

## Series resolution

Many commands need to identify which series to operate on. Kura
supports several input forms, collectively called **selector terms**.

**Text term** — a free-form string passed to the metadata provider for
fuzzy search. Multiple text terms may be combined; this is the primary
mechanism for resolving garbled or multi-language release titles.

**MetadataRef term** — an explicit `<provider>:<id>`, e.g.
`tvdb:370070`. Unambiguous. Must be the sole term; combining with other
terms is a "conflicting terms" error.

The `dir:` prefix has no special resolver meaning. `dir:本好きの下剋上`
is just a text term.

### Resolution outcomes

- **Resolved** — exactly one series identified. Command proceeds.
- **Unresolved** — multiple candidates. Command halts. Kura returns
  candidates with title, year, status, genres, and per-term match
  evidence. Caller picks one and retries with a MetadataRef term. Kura
  never auto-selects.
- **Not found** — no candidates. Command halts.
- **Error** — invalid term combinations or transport failures. Command
  halts.

### Multi-term behavior

When multiple text terms are provided, Kura queries each term against
the provider and aggregates. Each candidate accumulates evidence: which
terms matched, at what rank, match source when available, and
annotations such as `full_match` or `partial_match`. This evidence is
surfaced in unresolved responses for caller reasoning.

### Disambiguation as a cross-cutting pattern

Every operation that takes a selector inherits the four resolution
outcomes (Resolved, Unresolved, Not Found, Error). Unresolved is not
an error — it is a structured invitation for the caller to
disambiguate. The protocol is the same across all operations and
surfaces:

1. Caller invokes an operation with a selector.
2. Kura attempts resolution. If multiple candidates match, the
   operation halts and Kura returns the candidate set with
   provider-supplied metadata (title, year, status, genres) and
   per-term match evidence.
3. Caller picks one candidate and re-invokes the operation, replacing
   the selector with a MetadataRef term (e.g. `tvdb:<id>`). This
   bypasses fuzzy search entirely.
4. Operation proceeds.

The mechanics are identical for CLI and MCP; only the presentation
differs. A CLI invocation may render candidates as a prompt or a list
for the operator to choose from; an MCP call returns the candidates as
structured data for the agent to reason over and re-call. Kura itself
does not interact with the caller mid-operation — it returns the
candidate set and waits for the next call.

This is why MetadataRef terms are sole-term-only and authoritative:
they are the disambiguation answer, and they are how callers commit
after seeing candidates.

The same protocol applies to Not Found: the caller must refine the
query, supply additional terms, or escalate. Kura does not retry on the
caller's behalf.

## Naming convention

Canonical media filename:
`<title> - S<NN>E<NN> (<source> <resolution>).<ext>`

- `<title>` matches the series root directory name. Some drift between
  this and the provider title is tolerated; reconcile maintains
  consistency.
- `<source>` (e.g. `BDRip`, `WEBRip`, `TVRip`) is authoritative in
  series metadata.
- `<resolution>` is derived from mediainfo. Shorthand when possible:
  `4K`, `1440p`, `1080p`, `720p`, `480p`. Raw resolution is the
  fallback.
- Season and episode numbers are zero-padded to a minimum of two digits
  and expand as needed (`S01E150`).
- Codec, subtitle tracks, and audio tracks live in series metadata, not
  in the filename.
- Season 0 (specials) uses the same pattern: `<title> - S00E03 (...)`.
- Filesystem-hostile characters in titles are silently sanitized; rules
  below are normative.
- HDR/SDR is not P0.

Companion files: identical stem, different extension, with optional
language tag — e.g. `<title> - S01E01 (BDRip 1080p).ja.ass`.

Inside `series.json`, episodes are keyed by a fixed-width EpisodeRef of
form `S<NN>E<NNNN>` (e.g. `S01E0001`). Fixed width makes lexicographic
key order match natural episode order; the key is treated as opaque,
with the underlying season and episode integers carried on the episode
value. The on-disk filename grammar above is independent — it uses
min-2-digit padding for readability.

### Filename sanitization

POSIX-focused: targets ext4 / APFS / ZFS on Linux + macOS.
Cross-filesystem portability is handled via soft replacement of
Windows-hostile characters (legal on POSIX, rejected on NTFS/SMB) so
libraries stay accessible from Windows clients without enforcing
Windows-only constraints (CON/PRN/AUX reserved names, 260-char path
cap).

Sanitization is a total transform applied left-to-right:

1. **NFC normalize.** Compose decomposed sequences. APFS preserves
   what is written; HFS+ legacy NFD round-trips are accepted as a
   documented limitation, not transformed.
2. **Replace path separators.** `/` and `\` → space. POSIX rejects
   `/` in basenames; `\` is legal but pretends to be a separator on
   Windows.
3. **Replace colon.** `:` → ` -` (space-hyphen). Finder display layer
   remaps `:` to `/`; SMB-via-Windows clients reject it.
4. **Replace Windows-hostile chars.** `<`, `>`, `"`, `|`, `?`, `*` →
   space.
5. **Strip non-whitespace control chars** (NUL, DEL, etc.). Whitespace
   controls (`\t`, `\n`, `\r`) collapse to space.
6. **Collapse whitespace runs.** Any run of Unicode whitespace →
   single ASCII space.
7. **Trim leading/trailing whitespace and dots.** Trailing dots break
   Windows clients; leading dots make hidden files on POSIX.
8. **Reject empty result.** A title that sanitizes to empty errors at
   `add` / `import` time.

Length cap: composed canonical basenames are capped at 255 bytes (the
ext4 / APFS / ZFS limit). When the title portion would push the
basename past 255 bytes, it is truncated at a UTF-8 rune boundary;
episode marker, source, resolution, and extension are preserved
verbatim.

Path-length cap (4096 on Linux, ~1024 on macOS) is not enforced —
paths derive from operator-controlled library root + `Season N/`
prefix.

Implementation lives in `internal/domain/filename` (`Sanitize`,
`BuildMedia`, `MaxBasenameBytes`). Code is the implementation; this
section is the spec.

## Design model (internal invariants)

These are the load-bearing internal contracts. Most are not
user-visible; a misbehaving caller cannot break them. They appear here
so contributors and operators reading the spec know which constraints
are real.

- **Files on disk are state; metadata is spec.** The filesystem holds
  the actual media. Per-series metadata records what should be there
  and what was observed about each file. Kura's job is to surface the
  relationship between them and to converge them under explicit
  operator instruction. Inspired by git working tree vs. index, and
  Kubernetes spec vs. status.
- **Per-series metadata is the unit of truth.** Each series's metadata
  lives in its own directory and travels with it. Move a series
  directory to another library and it remains valid. Library-wide
  artifacts (the index) are caches — regenerable, not authoritative.
- **Local state is recoverable from filesystem + provider.** As long
  as the MetadataRef survives, any wedged or corrupt local state can
  be rebuilt by deleting the offending files and re-running the
  appropriate verb. Surgery (`rm` + Kura verb) is a first-class
  affordance, not a fallback. See [lifecycle.md §Recovery](lifecycle.md#recovery-and-surgery).
- **TVDB is authoritative; the spine is persisted.** `series.json`
  records every episode the provider knows about as a persisted spine:
  slot identity plus air date, and nothing richer. Scan re-syncs the
  spine from the provider. Drift is bounded by scan cadence.
- **Series titles are durable identity metadata.** `series.json`
  persists the provider-selected `preferredTitle` and `canonicalTitle`
  alongside the MetadataRef. Episode-level provider details remain
  bounded to slot identity plus air date.
- **State is observed, not stored.** "Episode is missing," "episode is
  staged," "episode file is gone" — these are derivations from
  (persisted spine × media records × staged records × filesystem
  reality), computed from local inputs at query time. Kura does not
  maintain a state machine.
- **Selectors, not paths.** Operations on tracked series identify
  their target by selector — text terms or MetadataRefs. Kura owns
  canonical path construction; callers never specify destination
  paths. The sole exception is `add`, which takes a literal SeriesRef
  because the series does not yet exist for a selector to resolve to.
- **Staging is a working tree; reconcile is the commit.** `stage`
  records intent in metadata. `reconcile` performs filesystem moves
  and updates metadata to reflect the new active state. `reset`
  discards staged intent. Reconcile is all-or-nothing in intent;
  failures are resolved by the operator/agent through standard verbs
  and surgery, not through automatic recovery.
- **Trash over delete.** Replaced files move to a per-series trash
  directory. Permanent deletion is always a separate, explicit action.
  Trash is the durability mechanism, not a convenience: reconcile is
  the most complex mutating operation in Kura and will have bugs, so
  displaced files must be recoverable. Same mechanism protects against
  agent misbehavior and Kura's own bugs.
- **Agent-reachable verbs are recoverable.** Every state mutation an
  agent can make is reversible from filesystem state plus existing
  artifacts: staged records can be reset, reconciles place displaced
  files in trash, scans re-derive metadata from reality. Permanent
  deletion (`trash empty`, `remove --purge`) requires operator action
  via CLI. Structural property, not convention.
- **Single binary, multiple surfaces.** Everything ships as `kura`.
  CLI verbs run as `kura <verb>`. Long-running surfaces run as
  `kura serve --mcp-stdio` / `--mcp-http=:port` / `--rest=:port`;
  flags can combine to host multiple transports under one process
  sharing one Coordinator + index cache.
- **Single writer at any moment.** Kura is built for single-replica
  deployment. The library has at most one active mutator process at a
  time. Multi-replica setups are not supported. The remaining same-host
  overlap between `kura serve` and manual `kura` CLI invocations is an
  accepted short-term risk; the structural fix is the
  CLI-as-REST-client migration once the REST API and web dashboard are
  fully wired up. See [deployment.md](deployment.md).
- **Kura returns facts; callers reason.** Upgrade candidacy,
  preference matching, fuzzy title disambiguation — these belong to
  the caller. Kura surfaces structured data and enforces invariants.
- **Reconcile is plan + apply.** `reconcile plan <selector>` computes
  and persists the change set under
  `<series>/.kura/reconcile/<token>.jsonl` and returns the token.
  `reconcile apply <selector> <token>` validates the snapshot and
  executes the moves. Plan inspection without commit is the dry-run
  affordance — there is no separate `--dry-run` flag. Other mutating
  operations are explicit one-step changes and do not need a dry-run
  mode.
- **Long workflows are job-shaped.** `Scan` and `ApplyReconcile` run
  via `internal/jobs/` regardless of surface. CLI blocks on the job
  and renders inline (operator sees no behavioral change). MCP / REST
  return a `JobHandle` to the client and let it poll `kura_job_status`
  / `GET /jobs/{id}`. Same registry, same lifecycle, same
  `KURA_JOB_TIMEOUT` bounds runaway operations across all surfaces.

## Hard invariants

These are the contracts Kura enforces at all times.

1. **One active media file per episode.** A second active file can
   only become active by staging with `--replace` and reconciling. No
   silent displacement.
2. **No multi-episode files.** A media file represents exactly one
   episode. Files packaging multiple episodes must be split before
   staging.
3. **Series directories are direct children of library root.** No
   nesting, no symlink following.
4. **Kura owns path construction.** Callers never specify destination
   paths. Canonical paths derive from series title, season, and
   episode number per the naming convention.
5. **`add` is the sole exception to selector-based addressing.** It
   takes a literal SeriesRef because the series does not yet exist for
   a selector to resolve to.
6. **Metadata writes are atomic.** Write to a temp file, rename. No
   partially-written `series.json`.
7. **State is derived from metadata, not probed live.** Local
   metadata records the spine and media records, not states.
   Observable states are derived at query time from `series.json`
   source data, including the source snapshots embedded in
   `index.jsonl`. Filesystem truth is reconciled by `scan`, not by
   read operations.
8. **Scan is disallowed on series with staged records.** Stage and
   scan write to the same metadata; running them concurrently or in
   the wrong order can clobber staged intent. Caller must `reconcile`
   or `reset` staged records before scanning.
9. **Reconcile is all-or-nothing in intent.** A failed reconcile
   leaves the series in an inconsistent state that the operator/agent
   resolves manually (via further `stage` / `reconcile`, or by surgery
   + `import` / `scan`). Kura does not automatically resume
   interrupted reconciles.
10. **Permanent deletion is always explicit.** `trash empty` requires
    deliberate invocation; library-wide invocation requires
    `--confirm`. **No other operation deletes trash contents** — not
    `scan`, not `reset`, not surgery-based recovery. The sole
    exception is `remove --purge`, which deletes the entire series
    directory wholesale and is treated as a deliberate operator
    action.
11. **Companion files follow their media record.** Replacement, trash,
    restore — companions move together with the media file they
    accompany.
12. **The persisted spine is bounded.** `series.json` stores only slot
    identity + air date for provider-derived episode data. No
    description, no synopsis, no images, no display titles.

## Jobs

Long-running workflows (`Scan`, `ApplyReconcile`, `Reindex`,
`ScanAll`) are submitted to the job registry in `internal/jobs/` and
exposed identically across surfaces.

- **CLI** blocks on the job and renders progress inline. The operator
  sees no behavioral difference from a synchronous call.
- **REST** returns `202 Accepted` with `{jobId, statusUrl, streamUrl}`.
  Clients poll `GET /api/v1/jobs/{id}` or stream
  `GET /api/v1/jobs/{id}/stream` (Server-Sent Events).
- **MCP** returns a job handle. Agents poll `kura_job_status`.

Lifecycle: each job has a ULID, kind (`scan` / `reconcile_apply` / …),
optional series ref, status (`running` / `succeeded` / `failed`),
result, end time, and a latest progress event. The registry retains
recently-terminal jobs for status polling. Per-job forensic logs are
written to `<library>/.kura/jobs/<ulid>.jsonl` and pruned by the sweep
goroutine after `KURA_LOG_RETENTION_DAYS` days (default 7).

`KURA_JOB_TIMEOUT` bounds individual job duration. Unset (or zero)
means no timeout.

## Out of scope

Items deliberately deferred or not handled today. Listed so the spec
is honest about its edges.

- **Bulk library queries.** "List all series with sub-1080p episodes,"
  "list all series using H264," etc. Each requires a full library
  walk. Infrequent enough to defer; no API shape committed.
- **Stale empty directories / abandoned series.** A tracked series
  with no files and no expressed intent to acquire — Kura treats this
  the same as a hole-filling target. No status flags.
- **Per-series intent flags** (watching/complete/abandoned). May be
  added later if needed for agent decisioning. Currently, the
  presence of a tracked series implies "wanted."
- **Cross-process concurrent mutation.** Resolved structurally by the
  CLI-as-REST-client architecture: `kura serve` is the sole writer;
  `kura` CLI becomes a thin REST client that never touches disk. Same-
  host overlap between server and CLI is no longer possible. NFS /
  SMB-mounted libraries are supported under the single-writer
  constraint; multi-replica deployment is explicitly out of scope.
- **In-place file replacement detection.** A file replaced in place
  without changing mtime/size is not re-mediainfo'd by `scan`.
  Explicit accepted limitation; full-rehash flag exists for paranoid
  mode.
- **HDR/SDR distinctions.** Not P0.
- **Audio language metadata.** Not tracked.
- **Movies.** Managed in a separate library outside Kura's scope; not
  addressed by these workflows or naming conventions.
- **Library-wide scan rate limits.** Scan makes a provider call per
  series; library-wide scan of a 1k-series library will be
  bottlenecked by provider rate limits, not just disk. Bulk-scan
  flows are expected to be operator-initiated or scheduled by
  `kura serve`'s MCP/REST consumer with appropriate pacing; no
  in-product rate-limiting affordance for now.
- **Multi-user, OIDC, scopes, federation.** Auth is a deploy-time
  bearer-token gate, not user identity. Multi-user concerns belong to
  an authenticating proxy (Authelia, oauth2-proxy, Caddy
  forward_auth). See [deployment.md](deployment.md).
