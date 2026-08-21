# REST API

Set `server.rest` in the library-manager TOML config to run the REST
transport. All endpoints are
under `/api/library/v1/`.

For underlying terms, see [concepts.md](concepts.md). For the
operations each endpoint implements, see
[lifecycle.md](lifecycle.md). For deployment, see
[deployment.md](deployment.md).

## Auth

None. Kura does not authenticate: no bearer token, no `[auth]` config,
no operator tier. Every route below is served to any client that can
reach the port.

**Stance:** access is a deployment concern, not an application one.
Front the server with an authenticating proxy and confine it with a
NetworkPolicy so only that proxy can route to it. See
[deployment.md](deployment.md#auth).

## CORS

Denied by default. Set `server.rest_cors_origins` to allow specific
browser origins.

```toml
[server]
rest = ":8080"
rest_cors_origins = ["https://ui.local"]
```

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
SeriesRef in the request body as `directory`.

## Endpoints

| Method | Path | Body | Response | Headers |
|--------|------|------|----------|---------|
| GET    | `/healthz` | — | `{ok, version, libraryRoot, uptimeMs, startedAt}` | — |
| GET    | `/api/library/v1` | — | Library summary | ETag |
| GET    | `/api/library/v1/series` | — | Paginated `ListResult` | ETag, query: `status`, `airing`, `tags`, `cursor`, `limit` |
| GET    | `/api/library/v1/series/{ref}` | — | `Show` (series + episodes) | ETag, query: `episodes`, `status`, `source`, `resolution` |
| PATCH  | `/api/library/v1/series/{ref}/tags` | `{tags[]}` | `{ref, tags[]}` | — |
| POST   | `/api/library/v1/series` | `{ref, directory?, ordering?}` | Series spine | — |
| POST   | `/api/library/v1/series/import` | `{ref, directory, force?, ordering?}` | Series spine | — |
| POST   | `/api/library/v1/series/{ref}/reset` | `{episode?, trash?, extras?, all?}` | Reset summary | — |
| POST   | `/api/library/v1/series/{ref}/scan` | `{refresh?, metadataOnly?, ordering?}` | `202 {jobId, kind, statusUrl, streamUrl, submittedAt}` | async |
| POST   | `/api/library/v1/series/{ref}/stage` | `{episodes[{episode, media, source?, companions?, replace?, attrs?}], trash[], extras[]}` | `202 Job` | async |
| POST   | `/api/library/v1/series/{ref}/reconcile/plan` | — | `{token, changes[], trashItems[], extras[]}` | — |
| POST   | `/api/library/v1/series/{ref}/reconcile/apply` | `{token}` | `202 Job` | async |
| POST   | `/api/library/v1/series/{ref}/reconcile/recover` | — | — | — |
| POST   | `/api/library/v1/series/resolve` | `{terms[]}` | Resolve candidates | — |
| GET    | `/api/library/v1/series/{ref}/trash` | — | Trash listing | ETag |
| GET    | `/api/library/v1/trash` | — | Trash across every indexed series | ETag |
| POST   | `/api/library/v1/series/{ref}/trash/{ulid}/restore` | — | Trash restore result | — |
| DELETE | `/api/library/v1/series/{ref}/trash` | — | Trash empty result | — |
| DELETE | `/api/library/v1/trash` | — | Empty result across every indexed series | — |
| POST   | `/api/library/v1/reindex` | — | `202 Job` | async |
| POST   | `/api/library/v1/scan` | `{refresh?, metadataOnly?, ordering?}` | `202 Job` | async |
| GET    | `/api/library/v1/inbox` | — | Inbox listing | ETag |
| GET    | `/api/library/v1/jobs/{job}` | — | Job status | — |
| GET    | `/api/library/v1/jobs/{job}/stream` | — | Server-Sent Events | 30 min max, 250 ms poll interval |

Episode stage entries accept optional `attrs`, a flat string map stored on
the staged media record. `GET /api/library/v1/series/{ref}` returns `attrs` on active
and staged media records when present; attrs are not queryable or indexed.
Active and staged media records also expose optional `dimensions` (the raw
`WIDTHxHEIGHT` value) and `modifiedAt` (the persisted file modification time in
RFC 3339 format) alongside the folded `resolution` label.
`GET /api/library/v1/series/{ref}?episodes=...` accepts `ALL`, `NONE`,
`AIRING_SEASON`, `S<N>`, `S<N>E<E>`, or `S<N>E<A>-<B>`. Empty means `ALL`.
`AIRING_SEASON` uses the same airing/tail window as list `isAiring` and
composes with `status`, `source`, and `resolution`.

Series tags are opaque workflow markers matching
`[a-z0-9][a-z0-9:_-]{0,63}`. Input is normalized to lowercase before
validation. `PATCH .../tags` applies plain expressions as additions and
`!tag` expressions as removals, atomically:

```json
{"tags":["priority","!maintenance-disabled"]}
```

`GET /api/library/v1/series?tags=priority%20!maintenance-disabled` applies a
conjunctive filter: every plain tag must be present and every `!tag` must be
absent. Multiple `tags` query parameters are concatenated. List and show
responses expose the stored tag set when non-empty.

Handlers live under `internal/server/rest/handler_*.go`. The router
and middleware chain (CORS, version header, recover) are in
`internal/server/rest/router.go` and `middleware.go`.

## Async jobs

Mutating long workflows (`scan`, `stage`, `reconcile apply`,
`reindex`, library `scan`) return `202 Accepted` with:

```json
{
  "jobId": "01HQF3XK...",
  "kind": "reconcile_apply",
  "statusUrl": "/api/library/v1/jobs/01HQF3XK...",
  "streamUrl": "/api/library/v1/jobs/01HQF3XK.../stream",
  "submittedAt": "2026-05-09T12:34:56Z"
}
```

Poll `statusUrl` or stream `streamUrl` to follow progress. The SSE
stream emits `progress` events while the job runs and a terminal
event with the result. The stream caps at 30 minutes and polls every
250 ms internally.

`jobs.timeout` bounds individual job duration; `"0s"` means no timeout.
Per-job forensic logs are written to
`<library>/.kura/jobs/<jobId>.jsonl` and pruned after
`sweep.log_retention_days` days (default 7).

## Version surfacing

The binary's version (stamped at build time via `-ldflags`) is
returned on `/healthz` and on every response as the
`X-Kura-Version` header. Build a versioned image with
`docker build --build-arg VERSION=v0.5.1 ...`; without the arg the
binary reports `dev`.
