# REST API

`kura serve --rest=:8080` runs the REST transport. All endpoints are
under `/api/v1/`.

For underlying terms, see [concepts.md](concepts.md). For the
operations each endpoint implements, see
[lifecycle.md](lifecycle.md). For deployment, see
[deployment.md](deployment.md).

## Auth

Bind safety is the bearer token: any reachable client must present
`Authorization: Bearer <token>`. Token resolution order:

1. `KURA_DISABLE_TOKEN=1` — auth bypassed entirely (use only when
   fronting `kura serve` with an authenticating proxy).
2. `KURA_TOKEN=<value>` — explicit env var.
3. `/var/lib/kura/token` — file persisted on first start. If absent,
   `kura serve` generates a 32-byte hex token, writes it (mode
   `0600`), and logs it once at INFO level. Subsequent restarts read
   the same file and do not regenerate.

The same secret protects REST and MCP-over-HTTP. MCP-stdio is
unauthenticated (the process boundary already trusts the parent).
`/api/v1/health` is exempt from auth.

**Stance:** auth is a deploy-time access gate, not user identity.
Multi-user, OIDC, scopes, and federation remain proxy responsibility.

## CORS

Denied by default. Pass `--rest-cors-origin=https://your-ui.example`
(repeatable) to allow specific browser origins.

```sh
kura serve --rest=:8080                                   # REST only
kura serve --rest=:8080 --mcp-stdio                       # REST + MCP stdio
kura serve --rest=:8080 --mcp-http=:8081 --rest-cors-origin=https://ui.local
```

## Operator gating

Per [concepts.md](concepts.md#actors), permanently-destructive verbs
are operator-only. REST enforces this with two headers:

- `X-Kura-Operator: 1` — required for trash mutations, purge remove,
  and reconcile recover.
- `X-Confirm: 1` — additionally required for trash empty and
  `remove --purge`.

Configure your auth proxy to strip these headers from external
requests so only trusted internal callers can invoke them.

## Reads, ETag

Read endpoints emit `ETag` headers based on content hash. Clients
sending `If-None-Match: <etag>` get `304 Not Modified` on unchanged
state. Useful for the bundled web dashboard and for any agent polling
in a loop.

## Resource refs

Per the "selectors, not paths" invariant
([concepts.md](concepts.md#design-model-internal-invariants)), the
resource-path `{ref}` is always a **MetadataRef** (provider:id, e.g.
`tvdb:370070`); the server resolves it to a SeriesRef via the index.
A SeriesRef in a path is rejected. `Add` and `Import` accept the
SeriesRef in the request body as `dirname`.

## Endpoints

| Method | Path | Body | Response | Headers |
|--------|------|------|----------|---------|
| GET    | `/api/v1/health` | — | `{ok, version, libraryRoot, uptimeMs, startedAt}` | none (auth-exempt) |
| GET    | `/api/v1/library` | — | Library summary | ETag |
| GET    | `/api/v1/series` | — | Paginated `ListResult` | ETag, query: `status`, `airing`, `cursor`, `limit` |
| GET    | `/api/v1/series/{ref}` | — | `Show` (series + episodes) | ETag, query: `episodes`, `status`, `source`, `resolution` |
| POST   | `/api/v1/series` | `{ref, dirname?, ordering?}` | Series spine | — |
| POST   | `/api/v1/series/import` | `{ref, dirname, force?, ordering?}` | Series spine | — |
| DELETE | `/api/v1/series/{ref}` | — | — | `X-Kura-Operator + X-Confirm` if `?purge=1`; no operator header for untrack-only removal |
| POST   | `/api/v1/series/{ref}/reset` | `{episode?, trash?, extras?, all?}` | Reset summary | — |
| POST   | `/api/v1/series/{ref}/scan` | `{refresh?, metadataOnly?, ordering?}` | `202 {jobId, kind, statusUrl, streamUrl, submittedAt}` | async |
| POST   | `/api/v1/series/{ref}/stage` | `{episodes[], trash[], extras[]}` | `202 Job` | async |
| POST   | `/api/v1/series/{ref}/reconcile/plan` | — | `{token, changes[], trashItems[], extras[]}` | — |
| POST   | `/api/v1/series/{ref}/reconcile/apply` | `{token}` | `202 Job` | async |
| POST   | `/api/v1/series/{ref}/reconcile/recover` | — | — | `X-Kura-Operator` |
| GET    | `/api/v1/series/{ref}/aliases` | — | `{aliases[]}` | ETag |
| POST   | `/api/v1/series/{ref}/aliases` | `{alias}` | — | — |
| DELETE | `/api/v1/series/{ref}/aliases` | `{alias}` | — | — |
| POST   | `/api/v1/resolve` | `{terms[]}` | Resolve candidates | — |
| GET    | `/api/v1/series/{ref}/trash` | — | Trash listing | ETag |
| GET    | `/api/v1/trash` | — | Library-wide trash | ETag |
| POST   | `/api/v1/series/{ref}/trash/{ulid}/restore` | — | Trash restore result | `X-Kura-Operator` |
| DELETE | `/api/v1/series/{ref}/trash` | — | Trash empty result | `X-Kura-Operator + X-Confirm` |
| DELETE | `/api/v1/trash` | — | Library-wide empty result | `X-Kura-Operator + X-Confirm` |
| POST   | `/api/v1/library/reindex` | — | `202 Job` | async |
| POST   | `/api/v1/library/scan` | `{refresh?, metadataOnly?, ordering?}` | `202 Job` | async |
| GET    | `/api/v1/inbox` | — | Inbox listing | ETag |
| GET    | `/api/v1/jobs/{job}` | — | Job status | — |
| GET    | `/api/v1/jobs/{job}/stream` | — | Server-Sent Events | 30 min max, 250 ms poll interval |

Handlers live under `internal/server/rest/handler_*.go`. The router
and middleware chain (auth, CORS, version header, recover) are in
`internal/server/rest/router.go` and `middleware.go`.

## Async jobs

Mutating long workflows (`scan`, `stage`, `reconcile apply`,
`reindex`, library `scan`) return `202 Accepted` with:

```json
{
  "jobId": "01HQF3XK...",
  "kind": "reconcile_apply",
  "statusUrl": "/api/v1/jobs/01HQF3XK...",
  "streamUrl": "/api/v1/jobs/01HQF3XK.../stream",
  "submittedAt": "2026-05-09T12:34:56Z"
}
```

Poll `statusUrl` or stream `streamUrl` to follow progress. The SSE
stream emits `progress` events while the job runs and a terminal
event with the result. The stream caps at 30 minutes and polls every
250 ms internally.

`KURA_JOB_TIMEOUT` bounds individual job duration. Unset means no
timeout. Per-job forensic logs are written to
`<library>/.kura/jobs/<jobId>.jsonl` and pruned after
`KURA_LOG_RETENTION_DAYS` days (default 7).

## Version surfacing

The binary's version (stamped at build time via `-ldflags`) is
returned on `/api/v1/health` and on every response as the
`X-Kura-Version` header. Build a versioned image with
`docker build --build-arg VERSION=v0.3.0 ...`; without the arg the
binary reports `dev`.
