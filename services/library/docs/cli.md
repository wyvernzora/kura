# CLI reference

`kura <verb>` operates on a library root configured via
`KURA_LIBRARY_ROOT`. Verbs that take a `<selector>` resolve text
terms or `tvdb:<id>` refs through the metadata provider; supplying a
metadata ref bypasses fuzzy search.

For underlying terms, see [concepts.md](concepts.md). For the journey
each verb implements, see [lifecycle.md](lifecycle.md).

## Operations catalog

The flat list of operations Kura exposes. Same semantics across all
surfaces unless noted. **Surface** columns: CLI, MCP, REST.

| Operation | Surface | Reason for exclusions | Purpose |
|---|---|---|---|
| `add <SeriesRef> [terms...]` | CLI + MCP + REST | — | Register a new series in the library: create directory `<SeriesRef>` and initialize metadata. The SeriesRef is the literal directory name to create; it also contributes as a text term in resolution. |
| `import <SeriesRef> [terms...]` | CLI + MCP + REST | — | Register identity on an existing untracked directory under library root. CLI exposes `--force` to overwrite a corrupted `.kura/series.json`; MCP and REST do not. |
| `scan <selector>` | CLI + MCP + REST | — | Re-sync local metadata with current reality. Hard-fails if the provider is unreachable. Job-shaped. |
| `stage <selector> <EpisodeRef> <path> [--replace] [companions...]` | CLI + MCP + REST | — | Record intent that `<path>` should become the active media file for the EpisodeRef. Files are not moved. |
| `reset <selector> [<EpisodeRef> \| --all]` | CLI + MCP + REST | — | Remove staged record(s). Does not touch staged files on disk. |
| `reconcile plan <selector>` | CLI + MCP + REST | — | Compute the planned filesystem changes for a series and persist them to `<series>/.kura/reconcile/<token>.jsonl`. Returns the plan plus a token (token is a hash of the series snapshot; apply re-validates the snapshot at execute time). No filesystem moves. |
| `reconcile apply <selector> <token>` | CLI + MCP + REST | — | Validate the persisted plan against current state and execute it. Job-shaped. All-or-nothing in intent; failures leave the series in an inconsistent state for manual resolution. |
| `reconcile recover <selector>` | CLI + REST (operator) | Operator judgment | Clear a stale `in_progress` claim left by a crashed `reconcile apply`. |
| `resolve <selector>` | CLI + MCP + REST | — | Resolve selector terms to candidate `MetadataRef`s. Returns the candidate list without auto-picking. |
| `list` | CLI + MCP + REST | — | Fast metadata inventory of the library. Untracked rows are surfaced on every surface. |
| `show <selector>` | CLI + MCP + REST | — | Return full observed state for a series. MCP and REST omit trash data. |
| `trash list <selector> \| --all` | CLI + REST (operator) | Safety boundary | List trashed files. `--older-than DURATION` filters by age. |
| `trash empty <selector> \| --all --confirm` | CLI + REST (operator + confirm) | Safety boundary | Permanently delete trashed files. The only verb that destroys content. |
| `trash restore <selector> <ULID>` | CLI + REST (operator) | Safety boundary | Move a trashed entry's files back to their recorded paths. Run `scan` afterward to re-adopt. |
| `reindex` | CLI + REST (operator) | Context efficiency | Walk library, regenerate `.kura/index.jsonl` source snapshots from per-series metadata. |
| `remove <selector> [--purge --confirm]` | CLI + REST (operator + confirm for `--purge`) | Operator judgment | Untrack a series. Default: delete `.kura/`, leave media. `--purge --confirm`: wholesale delete the entire series directory. |

Surface exclusions fall into three categories:

- **Context efficiency** — one-shot or low-frequency operator verbs
  that would cost agent context on every call for rare use.
- **Safety boundary** — verbs that permanently destroy data or
  surface trash state. Keeping them off MCP enforces the agent-safety
  property; REST exposes them only behind operator headers.
- **Operator judgment** — verbs that require human review of
  destructive consequences (`remove`, `reconcile recover`).

Bulk library queries (e.g. "list all series with sub-1080p episodes")
are deferred. Every such query requires a full library walk plus
per-file metadata inspection; they are infrequent and will be
designed when needed. `list` is the basic inventory exception: it
performs only the library root walk and per-series metadata reads.

## Selectors

Selectors are how every verb except `add` identifies its target
series. See [concepts.md §Series resolution](concepts.md#series-resolution)
for the full model. Quick reference:

- **Text term** — a free-form string for fuzzy provider search.
  Multiple text terms can be combined.
- **MetadataRef term** — `<provider>:<id>`, e.g. `tvdb:370070`.
  Unambiguous. Must be the sole term.
- **`dir:` prefix** — has no special meaning. `dir:something` is
  treated as a text term.

Resolution outcomes: **Resolved** (operation proceeds), **Unresolved**
(candidates returned, caller picks and retries with a MetadataRef),
**Not Found** (caller refines the query), **Error** (invalid term
combinations or transport failures).

## Series identity

| Verb | Purpose |
|------|---------|
| `kura add <selector> [--dirname NAME]` | Register a new series; create its directory and write the persisted spine. |
| `kura import <SeriesRef> [terms...]` | Adopt an existing untracked directory under the library root. |
| `kura remove <selector> [--purge --confirm]` | Untrack a series (default: drop `.kura/`, leave media). `--purge --confirm` wholesale deletes the directory. |

## Inspection

| Verb | Purpose |
|------|---------|
| `kura list [--status complete\|incomplete\|untracked\|error] [--airing\|--no-airing]` | Fast library inventory; `--status` repeatable. `--airing` / `--no-airing` filter by the independent airing flag. |
| `kura show <selector>` | Full observed state for one series, with per-episode mediainfo + filesystem-issue surfacing. |
| `kura resolve <selector> [--limit N]` | Resolve selector terms to candidate `MetadataRef`s without acting. |

## Episode lifecycle

| Verb | Purpose |
|------|---------|
| `kura scan <selector> [--replace]` | Re-sync a series with provider + filesystem; report orphan slots and skipped files. |
| `kura stage --episode S01E03 <selector> [--source WebRip] [--replace] [--companion PATH] <media-path>` | Record staged intent for one episode. Same-path stage is a metadata refresh and does not require `--replace`. |
| `kura reset --episode S01E03 <selector>` / `kura reset --all <selector>` | Drop one staged record or all of them. Does not touch staged files on disk. |
| `kura reconcile plan <selector>` | Compute and persist a reconcile plan under `<series>/.kura/reconcile/<token>.jsonl`; print the token. Same series state always produces the same token (snapshot-derived). Apply re-validates the snapshot at execute time, so a stale plan (series state changed) is rejected by token mismatch. |
| `kura reconcile apply <selector> <token>` | Validate the persisted plan against current state and execute the moves. |

## Trash

| Verb | Purpose |
|------|---------|
| `kura trash list <selector> \| --all [--older-than DURATION]` | Enumerate trashed entries for one series or library-wide. |
| `kura trash empty <selector> \| --all --confirm [--older-than DURATION]` | Permanently delete trashed files. `--all` requires `--confirm`. |
| `kura trash restore <selector> <ULID>` | Move a trashed entry's files back to their recorded paths. Run `scan` afterward to re-adopt. |

`--older-than` accepts `s/m/h/d/w` units (e.g. `30d`, `2w`, `48h`).

## Operator utilities

| Verb | Purpose |
|------|---------|
| `kura reindex` | Rebuild `.kura/index.jsonl` source snapshots from per-series metadata. |
| `kura reconcile recover <selector> [--force]` | Clear a stuck `in_progress` claim. Without `--force`, only breaks claims whose holder process is gone on the same host. |

## Output

Most verbs accept `--json` to force machine-readable output. Without
`--json`, output adapts to the terminal: TTY gets human tables,
non-TTY gets JSON.

## Typical flow

```sh
kura add <selector>                        # register a new series
kura scan <selector>                       # adopt existing files
kura stage --episode S01E03 <selector> --source WebRip /path/to/file.mkv
kura reconcile plan <selector>             # inspect what will move
kura reconcile apply <selector> <token>    # execute the plan
kura show <selector>                       # verify
kura trash list <selector>                 # review displaced files
kura trash empty <selector>                # permanently delete them
```

## Configuration

| Env / flag | Purpose |
|------------|---------|
| `KURA_LIBRARY_ROOT` | Library root directory (required). |
| `KURA_INBOX_ROOT` | Inbox root for staged downloads (required for `kura serve`). Must be disjoint from `KURA_LIBRARY_ROOT`. |
| `KURA_TVDB_KEY` | TVDB API key. Lazy: only required by provider-needing verbs (`add`, `import`, `scan`, `resolve`). |
| `KURA_PREFERRED_LANGUAGES` | Comma-separated BCP-47 preferred metadata languages. |
| `KURA_MEDIAINFO_COMMAND` | Override the `mediainfo` executable path. |
| `KURA_AIRING_TAIL_DAYS` | Integer days after a cour's last episode airs that the series still counts as airing. Default `7`; `0` disables the tail; empty / invalid / negative values fall back to default. |
| `KURA_HOST_ID` | Override `os.Hostname()` for claim holder identity. Set in container deployments to a stable value. |
| `KURA_UMASK` | Process umask for Kura-created files/directories and Kura-normalized moved media. Octal, e.g. `0022`, `0027`, or `0007`. Unset preserves the parent process default. |
| `KURA_TOKEN` / `KURA_DISABLE_TOKEN` | Server bearer token (see [deployment.md](deployment.md)). |
| `KURA_LOG_RETENTION_DAYS` | Days to retain forensic JSONL logs (reconcile plan + per-job). Default `7`. |
| `KURA_JOB_TIMEOUT` | Per-job deadline duration (e.g. `60m`). Unset means no timeout. |
| `--tvdb-base-url` | Override the TVDB API base URL (test/dev). |
