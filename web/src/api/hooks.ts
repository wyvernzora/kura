import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useDebounced } from '@/lib/useDebounced';

import { api } from './client';
import type {
  AddRequest,
  AddResult,
  ListResult,
  ListRow,
  Resolution,
  ResolveRequest,
  Show,
} from './types';
import type { SeriesTags, TagUpdate } from './types.gen';

const PAGE_SIZE = 100;

/**
 * Hard ceiling on cursor walks. 200 × 100 = 20 000 series — well past
 * any realistic personal library. Protects against a server that
 * returns a non-advancing cursor.
 */
const MAX_PAGES = 200;

/**
 * Walks `GET /api/v1/series` cursor pagination to assemble the full
 * library in one array. Personal-scale libraries (hundreds of series)
 * fit in memory cleanly; the virtualized grid handles render cost.
 *
 * Server-side search lands later; until then this hook owns the only
 * library copy on the client and the rest of the home page filters /
 * sorts / intersects against it.
 */
async function fetchAllSeries(): Promise<ListRow[]> {
  const acc: ListRow[] = [];
  let cursor: string | undefined;
  for (let i = 0; i < MAX_PAGES; i++) {
    const params = new URLSearchParams();
    params.set('limit', String(PAGE_SIZE));
    if (cursor) {
      params.set('cursor', cursor);
    }
    const page = await api<ListResult>(`/api/v1/series?${params.toString()}`);
    acc.push(...page.rows);
    if (!page.nextCursor || page.nextCursor === cursor) {
      return acc;
    }
    cursor = page.nextCursor;
  }
  return acc;
}

/**
 * Refetch cadence for the library list. Picked at 30 s as a balance
 * between picking up background mutations (an MCP agent staging /
 * reconciling while the dashboard is open) and not hammering the
 * server. Paired with the matching staleTime so a same-window
 * remount inside the interval serves cache without a redundant
 * fetch. `refetchIntervalInBackground` defaults to false, so polling
 * pauses while the tab is hidden.
 */
const SERIES_LIST_POLL_MS = 30_000;

export function useSeriesList() {
  return useQuery({
    queryKey: ['series'],
    queryFn: fetchAllSeries,
    staleTime: SERIES_LIST_POLL_MS,
    refetchInterval: SERIES_LIST_POLL_MS,
  });
}

const RESOLVE_DEBOUNCE_MS = 300;
const RESOLVE_MIN_QUERY_LENGTH = 2;

/**
 * Debounced wrapper around POST /api/v1/resolve. The library home
 * uses this to turn the user's search query into a ranked list of
 * metadata candidates; the home page then intersects those refs
 * against the loaded library to render matches in candidate order.
 *
 * Behavior:
 *   - Trims + debounces the query so a fast-typing user doesn't
 *     hammer TVDB.
 *   - Disables the query when the trimmed input is shorter than two
 *     characters; useQuery returns isPending = true with no fetch.
 *   - 60 s staleTime — resolve responses are stable for a given
 *     query.
 */
/**
 * Series detail fetch. `ref` is a metadata ref (provider:id, e.g.
 * `tvdb:370070`) — the same identifier the library list surfaces.
 *
 * The query is disabled when `ref` is undefined (e.g. route param
 * not yet hydrated) so consumers can call `useShow(ref)` from a
 * conditionally-mounted component without guarding.
 */
export function useShow(ref: string | undefined, preview = false) {
  return useQuery({
    queryKey: ['series', 'show', ref, preview],
    enabled: !!ref,
    staleTime: 30_000,
    queryFn: () =>
      api<Show>(`/api/v1/series/${encodeURIComponent(ref ?? '')}${preview ? '?preview=true' : ''}`),
  });
}

export function useResolveSearch(query: string) {
  const trimmed = query.trim();
  const debounced = useDebounced(trimmed, RESOLVE_DEBOUNCE_MS);
  const enabled = debounced.length >= RESOLVE_MIN_QUERY_LENGTH;
  return useQuery({
    queryKey: ['resolve', debounced],
    enabled,
    staleTime: 60_000,
    queryFn: () =>
      api<Resolution>('/api/v1/resolve', {
        method: 'POST',
        body: JSON.stringify({ terms: [debounced] } satisfies ResolveRequest),
      }),
  });
}

/**
 * Add a series to the library by metadata ref (POST /api/v1/series).
 * On success invalidates the library list so the new series lands in the
 * grid; callers navigate to the returned metadataRef.
 */
export function useAddSeries() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: AddRequest) =>
      api<AddResult>('/api/v1/series', {
        method: 'POST',
        body: JSON.stringify(body),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['series'] }),
  });
}

export function useUpdateTags(ref: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (tags: string[]) =>
      api<SeriesTags>(`/api/v1/series/${encodeURIComponent(ref)}/tags`, {
        method: 'PATCH',
        body: JSON.stringify({ tags } satisfies TagUpdate),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['series'] }),
  });
}
