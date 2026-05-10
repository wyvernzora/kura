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

### Container / Kubernetes setup

The published image is built `FROM gcr.io/distroless/cc-debian12:latest` — no shell, no package manager, no busybox. Only the `kura` binary, `mediainfo` plus its shared-library closure, and the `/var/lib/kura` bearer-token directory ship in the final layer. The kura binary itself is statically linked (`CGO_ENABLED=0`); only mediainfo pulls glibc-side dependencies.

The image runs as UID/GID baked at build time (default `10001:10001`). For NFS-backed library mounts where the export enforces a specific UID/GID, rebuild the image with matching values:

```sh
docker build \
  --build-arg KURA_UID=$(id -u) \
  --build-arg KURA_GID=$(id -g) \
  --build-arg VERSION=v0.1.0 \
  -t kura:v0.1.0 .
```

Three knobs flow through `--build-arg`:

| Arg | Default | Purpose |
|---|---|---|
| `KURA_UID` | `10001` | UID baked into `USER` directive and `/var/lib/kura` ownership. Match your NFS export's enforced UID. |
| `KURA_GID` | `10001` | GID counterpart. Match your NFS export's enforced GID (or use k8s `securityContext.fsGroup` to chown the mounted volume to runtime GID). |
| `VERSION` | `dev` | Stamped into the binary via `-ldflags`. Surfaces on `/api/v1/health` and the `X-Kura-Version` response header. |

Mount your library and inbox roots writable by that UID, and provide the bearer token via either a mounted volume or an env-injected secret:

| Path / env | Purpose | Recommendation |
|---|---|---|
| `/var/lib/kura/` (volume) | Persisted bearer token. Without persistence, `kura serve` regenerates a fresh token on every restart and previously-issued client configs break. | Mount a small PVC, **or** skip the volume and inject `KURA_TOKEN` from a Secret. The latter is preferred in k8s — explicit, version-controlled, no PVC lifecycle. |
| `KURA_LIBRARY_ROOT` | Library root (required). Series directories live here. | Mount a PV containing the library; both library and inbox roots must exist at start time and must not nest. |
| `KURA_INBOX_ROOT` | Inbox root for staged downloads (required for `kura serve`). | Same PV with a `subPath`, or a separate PV. Must be disjoint from `KURA_LIBRARY_ROOT`. |
| `KURA_HOST_ID` | Stable claim-stamp identity used by the boot-time stuck-claim recovery sweep. | **Set this** to a stable string (e.g. the underlying node hostname or a fixed deployment label). Without it, every container restart sees a different `os.Hostname()` and the auto-recovery can't break a prior pod's stale claim. |
| `KURA_TVDB_KEY` | TVDB API key. Lazy: only required for metadata-needing workflows. | Inject from a Secret. |
| `KURA_LOG_RETENTION_DAYS` | Days of forensic JSONL logs to keep (reconcile plan logs, per-job history logs). | Default `7`. Override only if you need longer retention for incident review. |

**Bootstrap on first start.** A fresh container with no `KURA_TOKEN` set and an empty `/var/lib/kura/` mount generates a 32-byte hex token, persists it to `/var/lib/kura/token` (mode 0600), and logs it once at INFO level — copy that into your client config. Subsequent restarts read the same file and do not regenerate. If you would rather manage the token out-of-band, set `KURA_TOKEN` from a Secret and skip the PVC entirely; the file path is then ignored.

**Stuck-claim recovery.** `kura serve` runs a one-shot recovery sweep at boot: it iterates the index, loads each series's `series.json`, and clears any `inProgress` claim whose holder's PID is gone on the same host. This is the auto-healing path for a pod that died mid-`reconcile apply` (OOMKill, eviction, rolling update). Cross-host stale claims and live same-host claims are logged but left alone; surface those manually with `kura reconcile recover --force` after confirming the prior writer is gone. The sweep depends on `KURA_HOST_ID` being stable across restarts — if you let it default to a per-container hostname, the new pod looks like a different host and the sweep treats every prior claim as cross-host.

**Health probe.** No Docker `HEALTHCHECK` directive — distroless ships no shell or `wget`, so any in-image probe would need its own static binary. For k8s, use a plain `httpGet` probe; no extra binary required:

```yaml
livenessProbe:
  httpGet:
    path: /api/v1/health
    port: 8080
  initialDelaySeconds: 20
  periodSeconds: 30
readinessProbe:
  httpGet:
    path: /api/v1/health
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

Adjust the port if you bind `--rest=:N` to anything other than `8080`.

**Runtime UID overrides.** `docker run --user X:Y` and k8s `securityContext.runAsUser` work but only if `/var/lib/kura` ownership inside the image matches `X:Y`, since distroless can't `chown` at runtime. Either rebuild the image with matching `KURA_UID`/`KURA_GID`, set `KURA_TOKEN` from a Secret to skip the file path entirely, or use k8s `securityContext.fsGroup` plus a PVC to have the kubelet chown the mount before the container starts.

**Building a versioned image.** `docker build --build-arg VERSION=v0.1.0 -t kura:v0.1.0 .` produces an image that reports `v0.1.0` on `/api/v1/health` and the `X-Kura-Version` response header. Without the arg the binary reports `dev`.

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
