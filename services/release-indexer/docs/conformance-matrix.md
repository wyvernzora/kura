# Conformance Matrix

The tagged conformance suite is intentionally small after the API-shape reset. It pins
the public contract that should not drift.

Run it with:

```sh
go test -tags=conformance ./...
```

## Current Coverage

| Contract | Test |
| --- | --- |
| Claim returns `claimToken`; submit `matched`; `list_releases` includes confidence | `TestAPIShape_MatchListsReleaseAndResolvesMagnet` |
| Single release detail is available over REST and dispatch, includes raw evidence and chronological match history, keeps magnet in detail, and orders `raw_items` by `id ASC` plus `match_events` by `created_at ASC, id ASC` | `TestAPIShape_GetReleaseDetail` |
| Release detail preserves absent facts as explicit JSON `null` (`ref`, `confidence`, `first_matched_at`, `magnet`, `size_bytes`, `url`), always renders `raw_items`/`match_events`/`sources` as arrays, and excludes lease internals (`claim_token`, `claimed_at`, `lease_expires_at`) | `TestAPIShape_GetReleaseExplicitNullsAndNoLeaseInternals` |
| `GET /api/releases/v1/{infohash}` maps malformed infohashes to `400 invalid_input` and unknown releases to `404 no_such_release` | `TestAPIShape_GetReleaseRESTErrors` |
| A stale `claim_token` cannot submit after a newer claim | `TestAPIShape_StaleClaimTokenRejected` |
| Repeated `unmatched` submissions exhaust after the configured max attempts | `TestAPIShape_UnmatchedExhaustsAfterMaxAttempts` |
| Migrations and the runtime pool land every migrated table, the goose version table, and the `match_status` enum in the configured `database.schema` and nothing in `public` | `TestConfiguredSchemaOwnsMigrationObjects` |
| DMHY fixtures, parser output, newest-200 bound, empty-floor threshold, and transient/parse failures | `sources/dmhy` conformance suite |
| Nyaa live fixtures, parser output, newest-window bound, empty-floor threshold, and transient failures | `sources/nyaa` conformance suite |
| Time-bounded scheduled walk: full >200-post backlog in one run, newest-plausible-stamp stop rule (sticky/epoch/future stamps), per-page emit with progress kept on failure, archive-floor and undatable-page termination | `pkg/crawl` `TestCrawlSince*` (unit) |
| Count-and-cursor crawl engine: exact mid-page budgets and resume, page-level lookback boundary resilient to pinned-old and epoch-artifact rows, empty-run resolution, confirmed floor, malformed/foreign cursors, transient failures without advanced state | `pkg/crawl` `TestCrawlChunk*`, `TestParseCursor*` (unit) |
| Shared source HTTP gate: serialized fetches, rolling three-request latency cooldown, capped transient retries and `Retry-After`, permanent-status rejection, cancellation | `pkg/crawl` `TestHTTPFetcher*` (unit/race) |
| `POST /api/releases/v1/sources/{source}/crawl`: direct ingest, cursor/lookback forwarding, stamp bounds, terminal response, upstream failure → 502 `upstream_error`, request validation | `internal/rest` `TestCrawlEndpoint*` (unit) |
| `kura crawl`: automatic bounded cursor loop, stdout checkpoints, and exact resume command through terminal response | `cli/cmd/kura` `TestCrawl*`, `cli/internal/cli/client` `TestCrawlSource*` (unit); `e2e` `TestEndToEndWorkflowCrawlCLI` |

The real-binary smoke test covers startup migrations, `/healthz`, `/api/releases/v1/ingest`,
`/api/releases/v1/{infohash}/magnet`, `/api/releases/v1/{infohash}`, `/api/releases/v1/queue/claim`, `/api/releases/v1/queue/submit`, `/api/releases/v1/queue/stats`
registration/call, removed worker path rejection, fail-fast bind behavior, strict TOML
startup, an in-process scheduled Nyaa crawl, direct ingest, and bounded shutdown.
The Docker e2e runs the consolidated release-indexer against fake DMHY and PostgreSQL,
then exercises the full claim, submit, and query workflow over those scheduled
releases.
The CLI crawl e2e builds the real `kura` and `kura-release-indexer` binaries, runs
them against stub DMHY and PostgreSQL, and covers bounded ingestion, idempotent
replay, client-driven cursor looping to a lookback boundary, and failure/resume
without gaps.
