import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '@/api/client';
import type {
  QueueStats,
  ReleaseDetail,
  ReleaseList,
  SetStatusRequest,
  SetStatusResult,
} from '@/api/releaseTypes';
import { type LoadedStream, type ReleaseStreamSpec, releaseListPath } from '@/state/releaseStreams';

/**
 * Release-indexer data hooks. The gateway's single origin is the
 * composition point: this page calls the indexer directly at
 * `/api/releases/v1/*`, and the library at `/api/library/v1/*`. No
 * bridge endpoint aggregates the two.
 *
 * Refresh is explicit — there is no polling here. The queue is
 * operator-paced at personal scale, and a list that reorders itself
 * under a decision is worse than a stale one with a Refresh button.
 */

const RELEASES_KEY = ['releases'] as const;

export interface ReleaseStream extends LoadedStream {
  isPending: boolean;
  isError: boolean;
  error: unknown;
  fetchNextPage: () => void;
  isFetchingNextPage: boolean;
}

/**
 * One cursor stream of the release list. `spec` is null when the
 * current filter does not need this stream, which disables the query
 * rather than unmounting the hook — hook order has to stay stable.
 */
export function useReleaseStream(spec: ReleaseStreamSpec | null): ReleaseStream {
  const query = useInfiniteQuery({
    queryKey: [
      ...RELEASES_KEY,
      'list',
      spec?.key ?? 'off',
      spec?.statuses.join(',') ?? '',
      spec?.maxConfidence ?? null,
    ],
    enabled: spec !== null,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      api<ReleaseList>(releaseListPath(spec as ReleaseStreamSpec, pageParam)),
    getNextPageParam: (last) => last.nextCursor || undefined,
    staleTime: 30_000,
  });
  return {
    key: spec?.key ?? 'plain',
    items: query.data?.pages.flatMap((page) => page.items) ?? [],
    hasMore: spec !== null && query.hasNextPage,
    isPending: spec !== null && query.isPending,
    isError: query.isError,
    error: query.error,
    fetchNextPage: () => void query.fetchNextPage(),
    isFetchingNextPage: query.isFetchingNextPage,
  };
}

/** Per-status counts for the filter menu. */
export function useQueueStats() {
  return useQuery({
    queryKey: [...RELEASES_KEY, 'stats'],
    staleTime: 30_000,
    queryFn: () => api<QueueStats>('/api/releases/v1/queue/stats'),
  });
}

/** Full record for one release: fields, source postings, match events. */
export function useReleaseDetail(infohash: string | null) {
  return useQuery({
    queryKey: [...RELEASES_KEY, 'detail', infohash],
    enabled: infohash !== null,
    staleTime: 30_000,
    queryFn: () => api<ReleaseDetail>(`/api/releases/v1/${encodeURIComponent(infohash ?? '')}`),
  });
}

export interface SetStatusInput {
  infohash: string;
  body: SetStatusRequest;
}

/**
 * Operator transition. Every action on this page is one of these:
 * hand match / correct / affirm submit `matched` with a ref, requeue
 * submits `unmatched` with none. Invalid transitions answer 409 and
 * the page surfaces that message verbatim.
 *
 * Invalidates the whole releases tree on settle — the row's status,
 * the open detail's event log and the menu's counts all moved.
 */
export function useSetReleaseStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ infohash, body }: SetStatusInput) =>
      api<SetStatusResult>(`/api/releases/v1/${encodeURIComponent(infohash)}/status`, {
        method: 'PUT',
        body: JSON.stringify(body),
      }),
    onSettled: () => qc.invalidateQueries({ queryKey: RELEASES_KEY }),
  });
}

/** Refetch everything this page reads, for the explicit Refresh control. */
export function useRefreshReleases(): () => void {
  const qc = useQueryClient();
  return () => void qc.invalidateQueries({ queryKey: RELEASES_KEY });
}
