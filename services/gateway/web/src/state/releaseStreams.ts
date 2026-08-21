import type { MatchStatus, ReleaseItem } from '@/api/releaseTypes';
import { LOW_CONFIDENCE, MATCH_STATUSES, RELEASE_PAGE_SIZE } from '@/lib/releases';

/**
 * Filter → request mapping and the client-side merge behind the
 * release list.
 *
 * `GET /api/releases/v1` filters by status server-side and by
 * `maxConfidence` server-side, but it cannot express "exhausted OR
 * (matched AND confidence < x)" in one query. So a filter selection
 * maps to at most two cursor streams — the plain statuses in one
 * comma-joined request, and the low-confidence pseudo-filter in its
 * own — and the merge below interleaves them newest-first.
 */

/** A status, or the client-side "Low confidence" pseudo-entry. */
export type ReleaseFilterKey = MatchStatus | 'lowconf';

export type StreamKey = 'plain' | 'lowconf';

export interface ReleaseStreamSpec {
  key: StreamKey;
  statuses: readonly MatchStatus[];
  /** Server-side ceiling: keeps rows with confidence != null && < this. */
  maxConfidence?: number;
}

/**
 * The streams a filter selection needs.
 *
 * An empty selection means "All", which still has to name every status
 * explicitly: the endpoint defaults to matched-only, so omitting the
 * parameter would silently hide four of the five states.
 */
export function streamsForFilter(active: ReadonlySet<ReleaseFilterKey>): ReleaseStreamSpec[] {
  const statuses =
    active.size === 0 ? MATCH_STATUSES : MATCH_STATUSES.filter((status) => active.has(status));
  const streams: ReleaseStreamSpec[] = [];
  if (statuses.length > 0) {
    streams.push({ key: 'plain', statuses });
  }
  if (active.has('lowconf')) {
    streams.push({ key: 'lowconf', statuses: ['matched'], maxConfidence: LOW_CONFIDENCE });
  }
  return streams;
}

/** Request path for one page of a stream. */
export function releaseListPath(
  spec: ReleaseStreamSpec,
  cursor?: string,
  limit: number = RELEASE_PAGE_SIZE,
): string {
  const params = new URLSearchParams();
  params.set('status', spec.statuses.join(','));
  if (spec.maxConfidence !== undefined) {
    params.set('maxConfidence', String(spec.maxConfidence));
  }
  params.set('limit', String(limit));
  if (cursor) {
    params.set('cursor', cursor);
  }
  return `/api/releases/v1?${params.toString()}`;
}

export interface LoadedStream {
  key: StreamKey;
  /** Pages flattened, newest first — the order the endpoint returns. */
  items: readonly ReleaseItem[];
  /** The server has at least one more page for this stream. */
  hasMore: boolean;
}

export interface MergedReleases {
  items: ReleaseItem[];
  /** More rows exist beyond `items`, either unfetched or withheld. */
  hasMore: boolean;
}

function publishedMs(item: ReleaseItem): number {
  const at = Date.parse(item.publishedAt);
  return Number.isNaN(at) ? 0 : at;
}

/**
 * Interleaves the loaded streams into one newest-first list, deduped
 * by infohash (a low-confidence match appears in both streams when
 * `matched` is also selected).
 *
 * Only the safe prefix is emitted. Each stream that still has pages
 * guarantees its unfetched rows are no newer than its last loaded row,
 * so anything at or above the newest such frontier is final; anything
 * below it could still be overtaken by a row that has not arrived, and
 * showing it would let the list reorder under the user. A stream that
 * has pages but nothing loaded yet withholds everything — its first
 * page could belong anywhere.
 */
export function mergeReleaseStreams(streams: readonly LoadedStream[]): MergedReleases {
  const frontiers = streams
    .filter((stream) => stream.hasMore)
    .map((stream) => {
      const last = stream.items[stream.items.length - 1];
      return last ? publishedMs(last) : Number.POSITIVE_INFINITY;
    });
  const cutoff = frontiers.length > 0 ? Math.max(...frontiers) : Number.NEGATIVE_INFINITY;

  const seen = new Set<string>();
  const merged: ReleaseItem[] = [];
  for (const stream of streams) {
    for (const item of stream.items) {
      if (seen.has(item.infohash)) {
        continue;
      }
      seen.add(item.infohash);
      merged.push(item);
    }
  }
  // Ties break on infohash DESC to match the server's page order — its seek
  // predicate is (key, infohash) < (cursor), so a later page's equal-timestamp
  // rows carry smaller infohashes and must sort below the ones already shown.
  merged.sort(
    (a, b) =>
      publishedMs(b) - publishedMs(a) ||
      (a.infohash < b.infohash ? 1 : a.infohash > b.infohash ? -1 : 0),
  );

  return {
    items: merged.filter((item) => publishedMs(item) >= cutoff),
    hasMore: streams.some((stream) => stream.hasMore),
  };
}

/**
 * Which stream to advance to extend the merged view. The blocking one
 * is whichever still-paged stream has the newest frontier — it is what
 * sets the cutoff, so fetching the other stream would add rows the
 * merge then withholds. Returns `null` when every stream is complete.
 */
export function nextStreamToLoad(streams: readonly LoadedStream[]): StreamKey | null {
  let pick: StreamKey | null = null;
  let newest = Number.NEGATIVE_INFINITY;
  for (const stream of streams) {
    if (!stream.hasMore) {
      continue;
    }
    const last = stream.items[stream.items.length - 1];
    const frontier = last ? publishedMs(last) : Number.POSITIVE_INFINITY;
    if (frontier > newest) {
      newest = frontier;
      pick = stream.key;
    }
  }
  return pick;
}
