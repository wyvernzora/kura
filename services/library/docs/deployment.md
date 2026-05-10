# Deployment

Kura ships as a single Go binary distributed via a distroless Docker
image. This doc covers the operational rules and the container /
Kubernetes setup.

For terminology, see [concepts.md](concepts.md). For the REST surface
exposed by `kura serve`, see [rest-api.md](rest-api.md).

## Single-writer rule

Kura is **single-writer by design**. Run a single `kura serve`
instance per library — multi-replica deployments are not supported.
Kura does not implement the cross-host coordination required to make
concurrent writers safe on a shared filesystem, and the homelab /
single-tenant shape it targets does not benefit from horizontal
scaling. For Kubernetes, use `replicas: 1` with `strategy: Recreate`.

Manual `kura` CLI invocations against the same library while
`kura serve` is running is an accepted short-term race window; today
the operator is responsible for not overlapping them. Future work
routes the CLI through the server's REST API so the server is the
sole writer.

The library may live on local disk, NFS, or SMB. Correctness depends
on the single-writer rule, not on the underlying filesystem.

## Auth

Bearer token, deploy-gate posture. Resolution order:

1. `KURA_DISABLE_TOKEN=1` — auth bypassed entirely. Use only when
   fronting `kura serve` with an authenticating proxy
   (Traefik+Authelia, nginx+oauth2-proxy, Caddy+forward_auth, etc.)
   that handles user identity.
2. `KURA_TOKEN=<value>` — explicit env var. Recommended for
   Kubernetes (inject from a Secret).
3. `/var/lib/kura/token` — file persisted on first start. If absent,
   `kura serve` generates a 32-byte hex token, writes it (mode
   `0600`), and logs it once at INFO level. Subsequent restarts read
   the same file.

Multi-user, OIDC, scopes, and federation remain proxy responsibility
— Kura deliberately does not implement them.

## Container / Kubernetes setup

The published image is built `FROM gcr.io/distroless/cc-debian12:latest`
— no shell, no package manager, no busybox. Only the `kura` binary,
`mediainfo` plus its shared-library closure, and the `/var/lib/kura`
bearer-token directory ship in the final layer. The `kura` binary
itself is statically linked (`CGO_ENABLED=0`); only `mediainfo`
pulls glibc-side dependencies.

The image runs as UID/GID baked at build time (default
`10001:10001`). For NFS-backed library mounts where the export
enforces a specific UID/GID, rebuild the image with matching values:

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

Mount your library and inbox roots writable by that UID, and provide
the bearer token via either a mounted volume or an env-injected
secret:

| Path / env | Purpose | Recommendation |
|---|---|---|
| `/var/lib/kura/` (volume) | Persisted bearer token. Without persistence, `kura serve` regenerates a fresh token on every restart and previously-issued client configs break. | Mount a small PVC, **or** skip the volume and inject `KURA_TOKEN` from a Secret. The latter is preferred in k8s — explicit, version-controlled, no PVC lifecycle. |
| `KURA_LIBRARY_ROOT` | Library root (required). Series directories live here. | Mount a PV containing the library; both library and inbox roots must exist at start time and must not nest. |
| `KURA_INBOX_ROOT` | Inbox root for staged downloads (required for `kura serve`). | Same PV with a `subPath`, or a separate PV. Must be disjoint from `KURA_LIBRARY_ROOT`. |
| `KURA_HOST_ID` | Stable claim-stamp identity used by the boot-time stuck-claim recovery sweep. | **Set this** to a stable string (e.g. the underlying node hostname or a fixed deployment label). Without it, every container restart sees a different `os.Hostname()` and the auto-recovery cannot break a prior pod's stale claim. |
| `KURA_TVDB_KEY` | TVDB API key. Lazy: only required for metadata-needing workflows. | Inject from a Secret. |
| `KURA_LOG_RETENTION_DAYS` | Days of forensic JSONL logs to keep (reconcile plan logs, per-job history logs). Default `7`. | Override only if you need longer retention for incident review. |
| `KURA_JOB_TIMEOUT` | Per-job deadline. Unset means no timeout. | Set if you want runaway jobs killed. |

### Bootstrap on first start

A fresh container with no `KURA_TOKEN` set and an empty
`/var/lib/kura/` mount generates a 32-byte hex token, persists it to
`/var/lib/kura/token` (mode `0600`), and logs it once at INFO level —
copy that into your client config. Subsequent restarts read the same
file and do not regenerate. If you would rather manage the token
out-of-band, set `KURA_TOKEN` from a Secret and skip the PVC entirely;
the file path is then ignored.

### Stuck-claim recovery

`kura serve` runs a one-shot recovery sweep at boot: it iterates the
index, loads each series's `series.json`, and clears any
`inProgress` claim whose holder's PID is gone on the same host. This
is the auto-healing path for a pod that died mid-`reconcile apply`
(OOMKill, eviction, rolling update). Cross-host stale claims and live
same-host claims are logged but left alone; surface those manually
with `kura reconcile recover --force` after confirming the prior
writer is gone. The sweep depends on `KURA_HOST_ID` being stable
across restarts — if you let it default to a per-container hostname,
the new pod looks like a different host and the sweep treats every
prior claim as cross-host.

### Health probe

No Docker `HEALTHCHECK` directive — distroless ships no shell or
`wget`, so any in-image probe would need its own static binary. For
k8s, use a plain `httpGet` probe; no extra binary required:

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

Adjust the port if you bind `--rest=:N` to anything other than
`8080`.

### Runtime UID overrides

`docker run --user X:Y` and k8s `securityContext.runAsUser` work but
only if `/var/lib/kura` ownership inside the image matches `X:Y`,
since distroless cannot `chown` at runtime. Either rebuild the image
with matching `KURA_UID` / `KURA_GID`, set `KURA_TOKEN` from a Secret
to skip the file path entirely, or use k8s
`securityContext.fsGroup` plus a PVC to have the kubelet chown the
mount before the container starts.

### Building a versioned image

```sh
docker build --build-arg VERSION=v0.1.0 -t kura:v0.1.0 .
```

Produces an image that reports `v0.1.0` on `/api/v1/health` and the
`X-Kura-Version` response header. Without the arg the binary reports
`dev`.
