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
| `matched -> dead` applies, records the reason to `match_events`, and preserves `ref` and `first_matched_at` | `TestDeadStatus_MatchedToDeadIsAllowedAndAudited` |
| Re-setting a status a release already has is an idempotent no-op that appends no second `match_events` row | `TestDeadStatus_RepeatIsAnIdempotentNoOp` |
| Transitions outside the table (`unmatched -> dead`, `dead -> matched`) are `ErrInvalidTransition`, and a missing release is `ErrNoSuchRelease` | `TestDeadStatus_TransitionsOutsideTheTableAreRejected` |
| A dead release stops resolving a magnet (proven against a matched pre-condition), moves from the `matched` to the `dead` count in `queue/stats`, and drops out of `list_releases` | `TestDeadStatus_MagnetGateAndQueueCounts` |
| `GET /api/releases/v1` with no filters stays matched-only, proven against a store that also holds suppressed and exhausted rows | `TestAttentionList_DefaultStaysMatchedOnly` |
| A `status` filter returns exactly those statuses, newest first, with `matchStatus` on every row, and still composes with `ref` | `TestAttentionList_StatusFilterReturnsTheAttentionSet` |
| `maxConfidence` is a strict `<` ceiling that excludes high scores, the ceiling value itself, and unscored matches | `TestAttentionList_MaxConfidenceSelectsLowScoringMatchesOnly` |
| A list cursor binds the status set and confidence ceiling: it resumes under identical filters and is `ErrInvalidCursor` under narrowed, dropped, added, or re-refed ones | `TestAttentionList_CursorBindsTheStatusAndConfidenceFilters` |
| Over REST: `status`/`maxConfidence` filter the page, the default stays matched-only, bad filters and `since` combinations are `400 invalid_request`, and a replayed cursor is `400 invalid_cursor` | `TestAttentionList_RESTFilterContract` |
| `suppressed -> matched` records the operator's ref at confidence `1.0`, stamps `first_matched_at`, and audits to `match_events` | `TestOperatorStatus_HandMatchFromSuppressed` |
| `exhausted -> matched` applies and opens the magnet gate, proven against a pre-condition where the exhausted release resolves none | `TestOperatorStatus_HandMatchFromExhaustedOpensTheMagnetGate` |
| `matched -> matched` affirms a low-confidence match to `1.0` with one audit row, is a no-op when repeated with the same ref, corrects to a new ref with a further row, and preserves `first_matched_at` | `TestOperatorStatus_AffirmAndCorrectAnExistingMatch` |
| `exhausted -> unmatched` resets `attempt_count`, clears the lease, returns the release to `queue/stats` availability and to `Claim`, and is an idempotent no-op when repeated | `TestOperatorStatus_RequeueMakesAnExhaustedReleaseClaimableAgain` |
| Per-target ref rules: `matched` without a ref is `ErrRefRequired`, with a malformed ref `ErrInvalidRef`, and `unmatched`/`dead` carrying a ref is `ErrRefForbidden` | `TestOperatorStatus_RefRules` |
| Transitions outside the table stay `ErrInvalidTransition` with or without a ref (`unmatched -> matched`, `suppressed -> unmatched`, `suppressed -> dead`, `matched -> unmatched`, `dead -> *`) | `TestOperatorStatus_TransitionsOutsideTheTableStayConflicts` |
| Over REST: hand match and requeue return 200, ref-rule violations are `400`, a malformed ref is `400 invalid_ref`, a table miss is `409 invalid_transition`, `suppressed` as a target is `400`, and a repeat hand match appends no second audit row | `TestOperatorStatus_RESTContract` |
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
| Cursor binding round-trip and rejection across ref, path, status set, and confidence ceiling | `internal/cursor` `TestEncodeDecode*`, `TestDecode*` (unit) |
| List/status request validation at the dispatch seam: closed status vocabulary, `(0,1]` confidence range, the `since` conflict, the narrower set-status target vocabulary, filter and ref forwarding, and the ref-rule error kinds | `internal/dispatch` `TestListReleases*`, `TestSetStatus*`, `TestErrorKind*` (unit) |
| `GET /api/releases/v1` query parsing for `status`/`maxConfidence` and their `400`s, and `matchStatus` on every rendered row | `internal/rest` `TestListReleases*` (unit) |

The real-binary smoke test covers startup migrations, `/healthz`, `/api/releases/v1/ingest`,
`/api/releases/v1/{infohash}/magnet` (including its pre-match `409 not_matched` gate), `/api/releases/v1/{infohash}`, `/api/releases/v1/queue/claim`, `/api/releases/v1/queue/submit`, `/api/releases/v1/queue/stats`
registration/call, removed worker path rejection, fail-fast bind behavior, strict TOML
startup, an in-process scheduled Nyaa crawl, direct ingest, and bounded shutdown.
The Docker e2e runs the consolidated release-indexer against fake DMHY and PostgreSQL,
then exercises the full claim, submit, and query workflow over those scheduled
releases.
The CLI crawl e2e builds the real `kura` and `kura-release-indexer` binaries, runs
them against stub DMHY and PostgreSQL, and covers bounded ingestion, idempotent
replay, client-driven cursor looping to a lookback boundary, and failure/resume
without gaps.
