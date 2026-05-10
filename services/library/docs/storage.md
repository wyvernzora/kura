# Storage formats

Reference for every JSON / JSONL file Kura writes. All are plain
files next to the media; you can read or fix them by hand if you need
to.

For the in-memory model, see [concepts.md](concepts.md). For the
workflows that produce these files, see [lifecycle.md](lifecycle.md).

## Layout

```
<library>/
  .kura/
    index.jsonl                     # library-wide row cache
    jobs/<ulid>.jsonl               # per-job forensic log (server only)
  <SeriesRef>/
    .kura/
      series.json                   # the per-series spine + records
      reconcile/<token>.jsonl       # planned + applied reconcile log
      trash/<ulid>/
        meta.json                   # describes one trashing event
        <displaced files>           # the actual displaced media + companions
    Season 01/
      <title> - S01E01 (BDRip 1080p).mkv
      <title> - S01E01 (BDRip 1080p).ja.ass
    ...
```

## Conventions

- All Kura-generated JSON files include a top-level `schemaVersion`.
- Writes are atomic: write to a temp file, rename. No partial JSON.
- Mutating writes are CAS-guarded (compare-and-swap on a content
  hash). The writer reads, mutates in memory, and writes only if the
  on-disk hash still matches; on conflict the writer reloads and
  retries. Implemented in `internal/coord/`.
- Every mutating write stamps a `mutator` tuple
  (`{op, pid, host, at}`) onto the file for forensic context.
- Every in-progress claim records a `holder` tuple
  (`{op, token, pid, host, started}`) on the series whose
  `reconcile apply` is running, cleared on success or by recover.

## `series.json` (schema v3)

Path: `<library>/<SeriesRef>/.kura/series.json`. The single canonical
metadata file for a series.

```jsonc
{
  "schemaVersion": 3,
  "metadataRef": "tvdb:370070",
  "preferredTitle": "Bocchi the Rock!",
  "canonicalTitle": "Bocchi the Rock!",
  "lastScanned": "2026-04-29T18:01:33Z",
  "ordering": "default",
  "artwork": { ... },
  "userAliases": ["bocchi"],
  "searchKey": "bocchirock",
  "episodes": {
    "S01E0001": {
      "airDate": "2022-10-08",
      "preferredTitle": "...",
      "canonicalTitle": "...",
      "active": { /* mediaRecord v1 */ },
      "staged": { /* mediaRecord v1 */ }
    },
    "S01E0002": { ... }
  },
  "stagedTrash":  [ { "id": "01HQF...", "path": "...", "size": 0, ... } ],
  "stagedExtras": [ { "id": "01HQG...", "path": "...", "season": 1, ... } ],
  "in_progress":  { "op": "reconcile_apply", "token": "...", "pid": 0, "host": "...", "started": "..." } | null,
  "last_mutated": { "op": "stage", "pid": 0, "host": "...", "at": "..." }
}
```

- Episode key format is `S<NN>E<NNNN>` (fixed-width). Lexicographic
  key order matches natural episode order.
- Each episode value carries `airDate` plus optional preferred /
  canonical titles for that episode.
- `active` and `staged` are mediaRecord v1: `{path, source,
  resolution, codec, size, mtime, companions[]}`. `path` is stored
  series-relative on disk and absolutized on load.
- `stagedTrash` and `stagedExtras` carry pre-reconcile intent for
  trash and extras-folder placements outside per-episode slots.
- Reads: `seriesfile.Load(libRoot, ref)`. Writes: `SaveCAS(libRoot,
  series, mutator)` returns the new content hash.

## `index.jsonl` (schema v2+)

Path: `<library>/.kura/index.jsonl`. Library-wide cache mapping
MetadataRefs to SeriesRefs plus per-series rollups for the `list`
verb. JSON-lines:

```jsonl
{"type":"header","version":2,"count":42}
{"type":"row","series":"Bocchi the Rock!","metadata":"tvdb:370070","title":"Bocchi the Rock!","status":"tracked"}
{"type":"row","series":"My Other Show","metadata":"","title":"My Other Show*","status":"untracked"}
...
```

- Header is line 1, then one row per visible direct child of the
  library root (tracked + untracked).
- Not authoritative. Regenerate from per-series metadata at any time
  via `kura reindex`.
- Read via `indexfile.Load()`. Mutated via `indexfile.SaveCAS()`.
  In-memory shape: `bySeries` map, `byMeta` selector lookup, sorted
  order slice. The server watches the file and reloads on peer
  mutation.

## Reconcile plan files (schema v2)

Path: `<library>/<SeriesRef>/.kura/reconcile/<token>.jsonl`. JSON-lines.

```jsonl
{"type":"header","token":"abcd1234ef56","createdAt":"2026-05-09T12:34:56Z","series":"<ref>","snapshot":"<sha256>"}
{"type":"step","id":"...","kind":"file_move","from":"...","to":"...","owner":{"intent":"episode","episodeRef":"S01E0003"}}
{"type":"step","id":"...","kind":"dir_remove","path":"..."}
{"type":"event","stepID":"...","at":"...","error":null}
{"type":"event","stepID":"...","at":"...","error":"..."}
{"type":"result","status":"success","appliedCount":17,"error":null}
```

- Token = first 12 hex chars of `SHA256(snapshot bytes)`. Same series
  state always produces the same token; idempotent re-plan. Apply
  re-validates the snapshot at execute time, so a plan whose series
  state has drifted is caught by token mismatch — there is no separate
  TTL.
- Step kinds: `file_move` (from/to series-relative slash form),
  `dir_remove` (path series-relative; silently skips if not empty).
- Owners: `OwnerEpisode` (canonical move), `OwnerTrash` (to
  `.kura/trash/<id>/`), `OwnerExtra` (`Season N/Extra/[Prefix]/`).
- Events appended during apply, terminal `result` line on completion
  (success or partial). The log is forensic — operators consult it
  during recovery.
- Pruned by the periodic sweep after `KURA_LOG_RETENTION_DAYS`
  (default 7).

## Trash `meta.json` (schema v1)

Path:
`<library>/<SeriesRef>/.kura/trash/<ULID>/meta.json`. Self-describing
forensic record of one trashing event:

```jsonc
{
  "schemaVersion": 1,
  "ulid": "01HQF3XK...",
  "episodeRef": "S01E0003",   // null for trash from non-episode sources
  "trashedAt": "2026-05-09T12:34:56Z",
  "record": {
    "path": "Season 01/<title> - S01E03 (WebRip 1080p).mkv",
    "source": "WebRip",
    "resolution": "1080p",
    "codec": "H264",
    "size": 1234567890,
    "mtime": "2026-04-01T00:00:00Z",
    "companions": ["..."]
  }
}
```

- One file per trashed media event. The presence of `meta.json`
  asserts that the trashing event completed.
- The displaced media file and companions live in the same ULID
  directory next to `meta.json`.
- Operator-managed via `kura trash list / empty / restore`. Never
  collateral to other operations.

## Per-job logs

Path: `<library>/.kura/jobs/<jobId>.jsonl`. Written by
`internal/jobs/` for every async workflow (Scan, ApplyReconcile,
Reindex, library Scan). Lifecycle events + result. Pruned by the
periodic sweep after `KURA_LOG_RETENTION_DAYS` (default 7).

## Path package

All path construction is centralized in
`internal/storage/paths/`: `LibraryKuraDir`, `IndexFile`,
`SeriesDir`, `SeriesKuraDir`, `SeriesMetadata`, `TrashDir`,
`TrashEntry`, `TrashMeta`, `PlanDir`, `PlanFile`, etc. Sibling
storage packages (`indexfile`, `seriesfile`, `planfile`,
`trashfile`) do not import each other; they all go through `paths`.
