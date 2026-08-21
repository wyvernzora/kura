/**
 * Release-indexer wire types (`/api/releases/v1/*`).
 *
 * Hand-written, unlike [./types.gen.ts](./types.gen.ts): tygo generation
 * is wired to the library service's `pkg/api` only, and the indexer is
 * a separate module behind the same gateway origin. Mirror
 * `services/release-indexer/pkg/api/{release,queue}.go` when it moves.
 *
 * Nullable database facts stay explicit nulls rather than being
 * omitted, so a consumer can tell "not recorded" from "not returned":
 * a release with no recorded size is not a release of size zero, and
 * an unscored match is not a match scored 0.0.
 */

export type MatchStatus = 'unmatched' | 'matched' | 'suppressed' | 'exhausted' | 'dead';

/** One row of `GET /api/releases/v1`. */
export interface ReleaseItem {
  infohash: string;
  /** Canonical ref, or empty when the release carries no match. */
  ref: string;
  title: string;
  sizeBytes: number | null;
  publishedAt: string;
  confidence: number | null;
  sources: string[];
  matchStatus: MatchStatus;
}

/** `GET /api/releases/v1`. Items is never null; the cursor is omitted on the last page. */
export interface ReleaseList {
  items: ReleaseItem[];
  nextCursor?: string;
}

/** One source posting linked to a release, ordered by id ascending. */
export interface RawItem {
  id: number;
  source: string;
  sourceId: string;
  title: string;
  url: string | null;
  publishedAt: string;
  ingestedAt: string;
}

/** One entry of a release's match history, ordered oldest first. */
export interface MatchEvent {
  id: number;
  status: MatchStatus;
  ref: string | null;
  confidence: number | null;
  reason: string | null;
  createdAt: string;
}

/** `GET /api/releases/v1/{infohash}`. */
export interface ReleaseDetail {
  infohash: string;
  title: string;
  magnet: string | null;
  sizeBytes: number | null;
  publishedAt: string;
  sources: string[];
  matchStatus: MatchStatus;
  ref: string | null;
  confidence: number | null;
  firstMatchedAt: string | null;
  attemptCount: number;
  createdAt: string;
  updatedAt: string;
  rawItems: RawItem[];
  matchEvents: MatchEvent[];
}

/** `GET /api/releases/v1/queue/stats` — the filter menu's per-status counts. */
export interface QueueStats {
  available: number;
  leased: number;
  unmatched: number;
  matched: number;
  suppressed: number;
  exhausted: number;
  dead: number;
}

/**
 * `PUT /api/releases/v1/{infohash}/status` body. `ref` carries the
 * hand-matched canonical ref; `reason` is recorded to match_events,
 * the existing audit trail. Invalid transitions answer 409 with the
 * standard error envelope.
 */
export interface SetStatusRequest {
  status: MatchStatus;
  ref?: string;
  reason?: string;
}

export interface SetStatusResult {
  ok: boolean;
}
