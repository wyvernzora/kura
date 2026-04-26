# Kura

Kura is an anime-first library manager inspired by tools like Sonarr.

The project is designed around anime as the primary use case. Other series types should work where the model fits, but anime conventions, release patterns, metadata, and automation workflows take priority.

The intended shape is deliberately lean:

- Single `kura` binary hosts every surface.
- CLI verbs for direct manual use (shipped).
- `kura serve --mcp-stdio` / `--mcp-http=:port` for agentic workflows (planned).
- `kura serve --rest=:port` for a future WebUI (planned).
- Docker-first distribution.

**Auth posture: deploy-gate, not user identity.** Kura ships a single shared-secret bearer token as a deploy-time access gate. Required by default; auto-generated and persisted at `/var/lib/kura/token` on first start. Set `KURA_DISABLE_TOKEN=1` to bypass when fronting `kura serve` with an authenticating proxy (Traefik+Authelia, nginx+oauth2-proxy, Caddy+forward_auth, etc.) that handles user identity. Multi-user, OIDC, scopes, and federation remain proxy responsibility — kura deliberately doesn't implement them.

## REST API

`kura serve --rest=:8080` runs the REST transport. Bind safety is the bearer token: any reachable client must present `Authorization: Bearer <token>`. Token comes from `KURA_TOKEN` env, then `/var/lib/kura/token`, else auto-generated and persisted at that path on first start. `KURA_DISABLE_TOKEN=1` opts out (use with an authenticating proxy in front).

CORS is denied by default; pass `--rest-cors-origin=https://your-ui.example` (repeatable) to allow specific browser origins.

```sh
kura serve --rest=:8080                                   # REST only
kura serve --rest=:8080 --mcp-stdio                       # REST + MCP stdio
kura serve --rest=:8080 --mcp-http=:8081 --rest-cors-origin=https://ui.local
```

Endpoint surface (all under `/api/v1/`). Per Product.md "Selectors,
not paths," resource-path `{ref}` is always a **MetadataRef**
(provider:id, e.g. `tvdb:370070`); the server resolves it to a
SeriesRef via the index. SeriesRef in a path is rejected. Add /
Import accept the SeriesRef in the request body as `dirname`.

| Method | Path | Workflow |
|--------|------|----------|
| GET    | `/health` | liveness + version + uptime |
| GET    | `/library` | server-level summary |
| GET    | `/series` | List (status/limit/cursor query) |
| GET    | `/series/{ref}` | Show (episodes/status/source/resolution query) |
| POST   | `/series` | Add — body: `{ref, dirname?, ordering?}` |
| POST   | `/series/import` | Import — body: `{ref, dirname, force?, ordering?}` |
| DELETE | `/series/{ref}` | Remove (`?purge=1` is operator-only) |
| POST   | `/series/{ref}/reset` | Reset |
| POST   | `/series/{ref}/scan` | Scan (async — returns 202 + jobId) |
| POST   | `/series/{ref}/stage` | Stage (async) |
| POST   | `/series/{ref}/reconcile/plan` | PlanReconcile |
| POST   | `/series/{ref}/reconcile/apply` | ApplyReconcile (async) |
| POST   | `/series/{ref}/reconcile/recover` | RecoverReconcile (operator) |
| POST   | `/resolve` | Resolve — body: `{terms: [...]}` |
| GET    | `/series/{ref}/trash`, `/trash` | TrashList |
| POST   | `/series/{ref}/trash/{ulid}/restore` | TrashRestore (operator) |
| DELETE | `/series/{ref}/trash`, `/trash` | TrashEmpty (operator + `X-Confirm: 1`) |
| POST   | `/library/reindex` | Reindex (operator) |
| GET    | `/jobs/{id}` | Job status (poll) |
| GET    | `/jobs/{id}/stream` | Job progress (SSE) |

Operator-only endpoints require `X-Kura-Operator: 1`. Destructive ones (trash empty, remove --purge) additionally require `X-Confirm: 1`. Configure your auth proxy to strip these from external requests so only trusted internal callers can invoke them. Reads emit ETag headers; clients with `If-None-Match` get 304 on unchanged state.

The CLI (`kura <verb>`) currently still talks direct-disk; migration to consume this REST surface is in progress on the `rest-api` branch.

## Deployment

Kura is **single-writer by design**. Run a single `kura serve` instance per library — multi-replica deployments are not supported. Kura does not implement the cross-host coordination required to make concurrent writers safe on a shared filesystem, and the homelab / single-tenant shape it targets does not benefit from horizontal scaling. For Kubernetes, use `replicas: 1` with `strategy: Recreate`.

Manual `kura` CLI invocations against the same library while `kura serve` is running is an accepted short-term race window; today the operator is responsible for not overlapping them. Future work routes the CLI through the server's REST API so the server is the sole writer.

The library may live on local disk, NFS, or SMB. Correctness depends on the single-writer rule, not on the underlying filesystem.

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
