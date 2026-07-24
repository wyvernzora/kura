# Deployment

The library manager ships as a serve-only Go binary distributed via an
Alpine-based Docker image. This doc covers the operational rules and the
container / Kubernetes setup.

For terminology, see [concepts.md](concepts.md). For the REST surface
exposed by the library-manager server, see [rest-api.md](rest-api.md).

## Single-writer rule

Kura is **single-writer by design**. Run a single library-manager
server per library — multi-replica deployments are not supported.
Kura does not implement the cross-host coordination required to make
concurrent writers safe on a shared filesystem, and the homelab /
single-tenant shape it targets does not benefit from horizontal
scaling. For Kubernetes, use `replicas: 1` with `strategy: Recreate`.

Normal `kura` CLI invocations talk to the server's REST API, so the server
remains the sole writer. Do not run a second library-manager server against
the same library.

The library may live on local disk, NFS, or SMB. Correctness depends
on the single-writer rule, not on the underlying filesystem.

## Auth

Bearer token, deploy-gate posture. Resolution order:

1. `auth.disabled = true` in TOML — auth bypassed entirely. Use only when
   fronting the library-manager server with an authenticating proxy
   (Traefik+Authelia, nginx+oauth2-proxy, Caddy+forward_auth, etc.)
   that handles user identity.
2. `KURA_TOKEN=<value>` — explicit env var. Recommended for
   Kubernetes (inject from a Secret).
3. `auth.token_path` (default `/var/lib/kura/token`) — file persisted
   on first start. If absent,
   the library-manager server generates a 32-byte hex token, writes it (mode
   `0600`), and logs it once at INFO level. Subsequent restarts read
   the same file.

Multi-user, OIDC, scopes, and federation remain proxy responsibility
— Kura deliberately does not implement them.

## Container / Kubernetes setup

The published image is built `FROM alpine:3.24`. `mediainfo`,
`ca-certificates`, and `tzdata` are installed via `apk` so apk pulls
the full dependency closure (libmediainfo, libzen, libcurl,
libtinyxml2, locale data, etc.). The `kura-library-manager` binary is
statically linked (`CGO_ENABLED=0`) and runs identically against musl. Alpine's
busybox shell + coreutils stay in the image so operators can
`kubectl exec` and inspect filesystem state when something breaks.

`ENTRYPOINT` is `kura-library-manager`; `CMD` defaults to
`["--config=/etc/kura/library-manager.toml"]`, so a pod or `docker run`
invocation with no `args:` / `command:` starts both transports using the
bundled config — REST on `:8080` and MCP-over-HTTP on `:8081`. Both use
`EXPOSE 8080 8081`. The same bearer token gates both. Mount a ConfigMap
or file at `/etc/kura/library-manager.toml` to change settings.

The image is serve-only. CLI verbs live in the separate top-level `cli/`
module, whose `kura` binary is a pure REST client configured through
`KURA_SERVER_URL` and `KURA_TOKEN`. Do not override the container's
`args:` to run CLI verbs; they are not part of this image.

If you only want REST (or only MCP), disable the unwanted transport in
TOML by setting its address to `""`.

The image runs as UID/GID baked at build time (default
`10001:10001`). For NFS-backed library mounts where the export
enforces a specific UID/GID, rebuild the image with matching values:

```sh
docker build \
  --build-arg KURA_UID=$(id -u) \
  --build-arg KURA_GID=$(id -g) \
  --build-arg VERSION=v0.5.1 \
  -t kura:v0.5.1 .
```

Three knobs flow through `--build-arg`:

| Arg | Default | Purpose |
|---|---|---|
| `KURA_UID` | `10001` | UID baked into `USER` directive and `/var/lib/kura` ownership. Match your NFS export's enforced UID. |
| `KURA_GID` | `10001` | GID counterpart. Match your NFS export's enforced GID (or use k8s `securityContext.fsGroup` to chown the mounted volume to runtime GID). |
| `VERSION` | `dev` | Stamped into the binary via `-ldflags`. Surfaces on `/api/v1/health` and the `X-Kura-Version` response header. |

Mount your library and inbox roots writable by that UID. Point the required
`library.root` and `library.inbox` settings at those container paths. The full,
commented schema with required markers and defaults lives in
[config.example.toml](../config.example.toml); startup rejects unknown fields.

The remaining environment variables are deliberately narrow:

| Environment variable | Purpose | Recommendation |
|---|---|---|
| `KURA_TOKEN` | Literal bearer secret. Takes precedence over `auth.token_path`. | Inject from a Secret in Kubernetes. |
| `KURA_TVDB_KEY` | TVDB API key. Lazy: only required for metadata-needing workflows. | Inject from a Secret. |
| `KURA_HOST_ID` | Stable claim-stamp identity used by the boot-time stuck-claim recovery sweep. | **Set this** to a stable string such as a node hostname or fixed deployment label. |

If `KURA_TOKEN` is absent and auth is enabled, mount `/var/lib/kura/` (or the
parent of your configured `auth.token_path`) to persist the generated token.
Without persistence, the server regenerates a token after each container
replacement.

Permission normalization after moving media is best-effort. On NFS
exports or Kubernetes security contexts that reject `chown` / `chmod`,
Kura keeps the successful move and relies on the operator to fix the
mount UID/GID, parent setgid bit, `server.umask`, or existing file modes.
For the intended single-writer personal-library deployment, this is an
operational repair, not a reason to roll back a 100+ GB move.

### Bootstrap on first start

A fresh container with no `KURA_TOKEN` set and an empty
`/var/lib/kura/` mount generates a 32-byte hex token, persists it to
`/var/lib/kura/token` (mode `0600`), and logs it once at INFO level —
copy that into your client config. Subsequent restarts read the same
file and do not regenerate. If you would rather manage the token
out-of-band, set `KURA_TOKEN` from a Secret and skip the PVC entirely;
the file path is then ignored.

### Stuck-claim recovery

The library-manager server runs a one-shot recovery sweep at boot: it
iterates the index, loads each series's `series.json`, and clears any
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

No Docker `HEALTHCHECK` directive — kubelet's `httpGet` probe against
`/api/v1/health` is the canonical liveness/readiness check across
both Docker and Kubernetes; embedding a probe binary would just
duplicate kubelet's behavior. For k8s:

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

Adjust the port if `server.rest` binds anything other than `:8080`.

### Runtime UID overrides

`docker run --user X:Y` and k8s `securityContext.runAsUser` work but
only if `/var/lib/kura` ownership inside the image matches `X:Y` (the
image's `chown` runs at build time; the runtime user is created
read-only). Either rebuild the image with matching `KURA_UID` /
`KURA_GID`, set `KURA_TOKEN` from a Secret to skip the file path
entirely, or use k8s `securityContext.fsGroup` plus a PVC to have the
kubelet chown the mount before the container starts.

### Building a versioned image

```sh
docker build --build-arg VERSION=v0.5.1 -t kura:v0.5.1 .
```

Produces an image that reports `v0.5.1` on `/api/v1/health` and the
`X-Kura-Version` response header. Without the arg the binary reports
`dev`.
