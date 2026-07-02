# CLI reference

`kura <verb>` talks to a running `kura serve --rest` instance. The CLI discovers
the server from `KURA_SERVER_URL`, defaulting to `http://127.0.0.1:8080`.

The server owns `KURA_LIBRARY_ROOT`, `KURA_INBOX_ROOT`, metadata provider
configuration, and all filesystem writes. Verbs that take a `<selector>` resolve
text terms or `tvdb:<id>` refs through the server; supplying a metadata ref
bypasses fuzzy search.

For underlying terms, see [concepts.md](concepts.md). For the journey
each verb implements, see [lifecycle.md](lifecycle.md).

## Operations catalog

The flat list of operations Kura exposes. Same semantics across all
surfaces unless noted. **Surface** columns: CLI, MCP, REST.

| Operation | Surface | Reason for exclusions | Purpose |
|---|---|---|---|
| `add <selector> [--dirname NAME]` | CLI + MCP + REST | — | Register a new series in the library: resolve metadata, create a directory, and initialize metadata. `--dirname` overrides the directory name. |
| `import <dirname> [terms...]` | CLI + MCP + REST | — | Register identity on an existing untracked directory under library root. CLI and REST expose `force` to overwrite a corrupted `.kura/series.json`; MCP does not. |
| `scan <selector>` | CLI + MCP + REST | — | Re-sync local metadata with current reality. Hard-fails if the provider is unreachable. Job-shaped. |
| `stage episode|trash|extra ...` | CLI + MCP + REST | — | Record staged intent for episode media, queued trash, or extras. Files are not moved. |
| `reset <selector> [--episode S01E03 \| --trash ULID \| --extra ULID \| --all]` | CLI + MCP + REST | — | Remove staged record(s). Does not touch staged files on disk. |
| `reconcile plan <selector>` | CLI + MCP + REST | — | Compute the planned filesystem changes for a series and persist them to `<series>/.kura/reconcile/<token>.jsonl`. Returns the plan plus a token (token is a hash of the series snapshot; apply re-validates the snapshot at execute time). No filesystem moves. |
| `reconcile apply <selector> <token>` | CLI + MCP + REST | — | Validate the persisted plan against current state and execute it. Job-shaped. All-or-nothing in intent; failures leave the series in an inconsistent state for manual resolution. |
| `reconcile recover <selector>` | CLI + REST (operator) | Operator judgment | Clear a stale `in_progress` claim left by a crashed `reconcile apply`. |
| `resolve <selector>` | CLI + MCP + REST | — | Resolve selector terms to candidate `MetadataRef`s. Returns the candidate list without auto-picking. |
| `list` | CLI + MCP + REST | — | Fast metadata inventory of the library. Untracked rows are surfaced on every surface. |
| `show <selector>` | CLI + MCP + REST | — | Return full observed state for a series. Agent-facing surfaces omit permanent trash listings. |
| `trash list <selector> \| --all` | CLI + REST (operator) | Safety boundary | List trashed files. `--older-than DURATION` filters by age. |
| `trash empty <selector> \| --all --confirm` | CLI + REST (operator; REST also requires confirm) | Safety boundary | Permanently delete trashed files. CLI requires `--confirm` only with `--all`. |
| `trash restore <selector> <ULID>` | CLI + REST (operator) | Safety boundary | Move a trashed entry's files back to their recorded paths. Run `scan` afterward to re-adopt. |
| `reindex` | CLI + REST | Context efficiency | Walk library, regenerate `.kura/index.jsonl` source snapshots from per-series metadata. |
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
projects rows from the server's library index.

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
| `kura import <dirname> [terms...]` | Adopt an existing untracked directory under the library root. |
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
| `kura scan <selector> [--refresh] [--metadata-only] [--ordering ORDERING]` | Re-sync a series with provider + filesystem; report orphan slots and skipped files. |
| `kura stage episode <selector> S01E03 <inbox:media> [--source WebRip] [--replace] [--companion inbox:PATH]` | Record staged intent for one episode. Same-path stage is a metadata refresh and does not require `--replace`. |
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
export KURA_SERVER_URL=http://127.0.0.1:8080

kura add <selector>                        # register a new series
kura scan <selector>                       # adopt existing files
kura stage episode <selector> S01E03 inbox:path/to/file.mkv --source WebRip
kura reconcile plan <selector>             # inspect what will move
kura reconcile apply <selector> <token>    # execute the plan
kura show <selector>                       # verify
kura trash list <selector>                 # review displaced files
kura trash empty <selector>                # permanently delete them
```

## Configuration

| Env / flag | Purpose |
|------------|---------|
| `KURA_SERVER_URL` | REST server URL for CLI commands. Default `http://127.0.0.1:8080`. |
| `KURA_TOKEN` / `KURA_DISABLE_TOKEN` | Bearer token behavior for both server and CLI. See [deployment.md](deployment.md). |
| `KURA_LIBRARY_ROOT` | Server-side library root directory (required by `kura serve`). |
| `KURA_INBOX_ROOT` | Server-side inbox root for staged downloads (required by `kura serve`). Must be disjoint from `KURA_LIBRARY_ROOT`. |
| `KURA_TVDB_KEY` | Server-side TVDB API key. Required by provider-needing verbs (`add`, `import`, `scan`, `resolve`). |
| `KURA_PREFERRED_LANGUAGES` | Server-side comma-separated BCP-47 preferred metadata languages. |
| `KURA_MEDIAINFO_COMMAND` | Server-side override for the `mediainfo` executable path. |
| `KURA_AIRING_TAIL_DAYS` | Server-side integer days after a cour's last episode airs that the series still counts as airing. Default `7`; `0` disables the tail; empty / invalid / negative values fall back to default. |
| `KURA_HOST_ID` | Server-side override for claim holder identity. Set in container deployments to a stable value. |
| `KURA_UMASK` | Server-side process umask for Kura-created files/directories and Kura-normalized moved media. Octal, e.g. `0022`, `0027`, or `0007`. |
| `KURA_LOG_RETENTION_DAYS` | Server-side days to retain forensic JSONL logs (reconcile plan + per-job). Default `7`. |
| `KURA_JOB_TIMEOUT` | Server-side per-job deadline duration (e.g. `60m`). Unset means no timeout. |
| `--tvdb-base-url` | Server-side TVDB API base URL override (test/dev). |
