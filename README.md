# Kura

Kura is an anime-first library manager inspired by tools like Sonarr.

The project is designed around anime as the primary use case. Other series types should work where the model fits, but anime conventions, release patterns, metadata, and automation workflows take priority.

The intended shape is deliberately lean:

- CLI tools for direct manual use.
- MCP tools for agentic workflows (planned).
- Docker-first distribution.
- A UI only if it becomes clearly worth building later.

## CLI

`kura <verb>` operates on a library root configured via `KURA_LIBRARY_ROOT`. Verbs that take a `<selector>` resolve text terms or `tvdb:<id>` refs through the metadata provider; supplying a metadata ref bypasses fuzzy search.

### Series identity

| Verb | Purpose |
|------|---------|
| `kura add <selector> [--dirname NAME]` | Register a new series; create its directory and write the persisted spine. |
| `kura import <SeriesRef> [terms...]` | Adopt an existing untracked directory under the library root. |
| `kura remove <selector> [--purge --confirm]` | Untrack a series (default: drop `.kura/`, leave media). `--purge --confirm` wholesale deletes the directory. |

### Inspection

| Verb | Purpose |
|------|---------|
| `kura list [--status complete\|incomplete\|airing\|untracked\|error]` | Fast library inventory; `--status` repeatable. |
| `kura show <selector>` | Full observed state for one series, with per-episode mediainfo + filesystem-issue surfacing. |
| `kura resolve <selector> [--limit N]` | Resolve selector terms to candidate `MetadataRef`s without acting. |

### Episode lifecycle

| Verb | Purpose |
|------|---------|
| `kura scan <selector> [--replace]` | Re-sync a series with provider + filesystem; report orphan slots and skipped files. |
| `kura stage --episode S01E03 <selector> [--source WebRip] [--replace] [--companion PATH] <media-path>` | Record staged intent for one episode. Same-path stage is a metadata refresh and does not require `--replace`. |
| `kura reset --episode S01E03 <selector>` / `kura reset --all <selector>` | Drop one staged record or all of them. Does not touch staged files on disk. |
| `kura reconcile plan <selector>` | Compute and persist a five-minute reconcile plan under `<series>/.kura/reconcile/<token>.jsonl`; print the token. Same series state always produces the same token (snapshot-derived). |
| `kura reconcile apply <selector> <token>` | Validate the persisted plan against current state and execute the moves. |

### Trash

| Verb | Purpose |
|------|---------|
| `kura trash list <selector> \| --all [--older-than DURATION]` | Enumerate trashed entries for one series or library-wide. |
| `kura trash empty <selector> \| --all --confirm [--older-than DURATION]` | Permanently delete trashed files. `--all` requires `--confirm`. |
| `kura trash restore <selector> <ULID>` | Move a trashed entry's files back to their recorded paths. Run `scan` afterward to re-adopt. |

`--older-than` accepts `s/m/h/d/w` units (e.g. `30d`, `2w`, `48h`).

### Operator utilities

| Verb | Purpose |
|------|---------|
| `kura reindex` | Rebuild `.kura/index.tsv` from per-series metadata. |

### Output

Most verbs accept `--json` to force machine-readable output. Without `--json`, output adapts to the terminal: TTY gets human tables, non-TTY gets JSON.

### Typical flow

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
| `KURA_TVDB_KEY` | TVDB API key. Lazy: only required by provider-needing verbs (`add`, `import`, `scan`, `resolve`). |
| `KURA_PREFERRED_LANGUAGES` | Comma-separated BCP-47 preferred metadata languages. |
| `KURA_MEDIAINFO_COMMAND` | Override the `mediainfo` executable path. |
| `--tvdb-base-url` | Override the TVDB API base URL (test/dev). |

## Requirements

- Go 1.26.2 or newer.
- `mediainfo` on `PATH` (or set `KURA_MEDIAINFO_COMMAND`).
- Docker, if building or running the container image.

## Build / install

```sh
make build      # builds bin/kura
make install    # rebuilds and installs to $(go env GOBIN) or $GOPATH/bin
```

## Development checks

```sh
make check
```

Runs `gofmt`, `go vet`, `gopls check`, `go test`, and a local binary build. `gopls check` surfaces editor diagnostics such as Go's `modernize` analyzer warnings.

## Docker

```sh
docker build -t kura .
docker run --rm kura
```
