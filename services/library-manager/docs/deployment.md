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

Kura does not authenticate. There is no bearer token, no `[auth]`
config, and no operator tier — a request that reaches the service is
served.

That makes the boundary in front of it load-bearing. Run the
library-manager server behind an authenticating proxy, and confine it
with a NetworkPolicy (or equivalent) so nothing else can route to the
Pod directly. Identity, multi-user, OIDC, scopes, and federation are
all that proxy's responsibility.

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
invocation with no `args:` / `command:` starts REST on `:8080` and the
Prometheus `/metrics` listener on `:9090` using the bundled config. It
uses `EXPOSE 8080 9090`. A NetworkPolicy that lets Prometheus scrape
`:9090` grants no API access — the metrics listener serves `/metrics`
and nothing else (see [metrics.md](metrics.md)). Mount a ConfigMap or
file at `/etc/kura/library-manager.toml` to change settings.

The image is serve-only. CLI verbs live in the separate top-level `cli/`
module, whose `kura` binary is a pure REST client configured through
`KURA_SERVER_URL`. Do not override the container's
`args:` to run CLI verbs; they are not part of this image.

REST is the service's only transport; the suite MCP surface is the gateway's.

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
| `KURA_UID` | `10001` | UID baked into the `USER` directive. Match your NFS export's enforced UID. |
| `KURA_GID` | `10001` | GID counterpart. Match your NFS export's enforced GID (or use k8s `securityContext.fsGroup` to chown the mounted volume to runtime GID). |
| `VERSION` | `dev` | Stamped into the binary via `-ldflags`. Surfaces on `/healthz` and the `X-Kura-Version` response header. |

Mount your library and inbox roots writable by that UID. Point the required
`library.root` and `library.inbox` settings at those container paths. The full,
commented schema with required markers and defaults lives in
[config.example.toml](../config.example.toml); startup rejects unknown fields.

The remaining environment variables are deliberately narrow:

| Environment variable | Purpose | Recommendation |
|---|---|---|
| `KURA_TVDB_KEY` | TVDB API key. Lazy: only required for metadata-needing workflows. | Inject from a Secret. |
| `KURA_HOST_ID` | Stable claim-stamp identity used by the boot-time stuck-claim recovery sweep. | **Set this** to a stable string such as a node hostname or fixed deployment label. |

Permission normalization after moving media is best-effort. On NFS
exports or Kubernetes security contexts that reject `chown` / `chmod`,
Kura keeps the successful move and relies on the operator to fix the
mount UID/GID, parent setgid bit, `server.umask`, or existing file modes.
For the intended single-writer personal-library deployment, this is an
operational repair, not a reason to roll back a 100+ GB move.

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
`/healthz` is the canonical liveness/readiness check across
both Docker and Kubernetes; embedding a probe binary would just
duplicate kubelet's behavior. For k8s:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 20
  periodSeconds: 30
readinessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

Adjust the port if `server.rest` binds anything other than `:8080`.

### Runtime UID overrides

`docker run --user X:Y` and k8s `securityContext.runAsUser` work, but
the library and inbox mounts must be writable by `X:Y`. Either rebuild
the image with matching `KURA_UID` / `KURA_GID`, or use k8s
`securityContext.fsGroup` to have the kubelet chown the mount before
the container starts.

### Building a versioned image

```sh
docker build --build-arg VERSION=v0.5.1 -t kura:v0.5.1 .
```

Produces an image that reports `v0.5.1` on `/healthz` and the
`X-Kura-Version` response header. Without the arg the binary reports
`dev`.
