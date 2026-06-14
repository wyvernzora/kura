List series under the library root with summary state per row: status, airing flag, staged flag, episode counts, quality rollups, artwork, and last scan time.

The `staged` flag is independent of status — true if any episode has staged media awaiting reconcile.

Pagination: pass `maxResults` (default 100, max 1000) to cap the page size. The response includes a `nextCursor` token when there are more rows; pass it back as `cursor` for the next page. `dataChanged: true` flags that the underlying index changed between pages — clients can re-render from the current page if strict ordering matters.

<!-- schema-note
Parameter schema is defined in tool_list.go (jsonschema tags on listInput struct).
That Go definition is authoritative. If this section conflicts with the Go file, the Go file wins.
-->
## Parameters

<!-- schema -->
- `statuses` ([]string, optional) — filter by status. Allowed values: `complete`, `incomplete`, `error`, `untracked`. Empty or omitted returns all four.
- `airing` (bool, optional) — when true, return only currently-airing series; when false, return only non-airing series. Omit for no airing filter.
- `maxResults` (int, optional) — maximum rows per response. `0` or omitted uses the server default (100). Values above 1000 are clamped.
- `cursor` (string, optional) — opaque pagination token from a previous response's `nextCursor`. Omit for the first page.
<!-- /schema -->

## Pagination

`kura_list` returns one page, not the whole library. Treat each call as a slice.

**Response fields:**
- `rows` — this page only.
- `nextCursor` — present iff more rows remain. Absent or empty = last page. Stop iterating.
- `dataChanged` — true when the underlying library mutated between the prior page and this one (a series was added/removed/re-titled, or filter membership shifted). Cursor still resolves; pagination still completes; ordering across the boundary may have shifted.

**Iteration rules:**
1. Loop until `nextCursor` is absent. Don't infer "done" from row count alone — a final page can be exactly `maxResults` rows.
2. Pass the cursor back verbatim. It's case-insensitive base32; do not modify, trim, or re-encode.
3. If a call returns `invalid_cursor`, restart from page 1 (no cursor); the response will set `dataChanged: true`.
4. If a call returns `server_not_ready`, wait a few seconds and retry. Do not loop tightly.
5. Tighten the filter (`statuses`) instead of paginating through irrelevant rows when you know what you're looking for.
6. Do not cache cursors across sessions or long delays.

**`dataChanged` handling:**
- For triage / read-only summaries: keep walking; report final counts with a one-line note that the library shifted mid-walk.
- For "find series X" lookups: if the page you're on doesn't contain X and `dataChanged` is true, restart from page 1 — X may have moved earlier in the order.
- Heavy mutation in flight makes `dataChanged: true` likely on every page. Acceptable; not an error.
