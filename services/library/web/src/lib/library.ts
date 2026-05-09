import type { Candidate, ListRow } from '@/api/types';
import type { Status } from '@/lib/status';

/**
 * Sortable columns the library home exposes via the sort dropdown.
 * Wire-friendly stable strings so URL sync (P1+) can persist them.
 */
export type SortKey = 'title' | 'episodes' | 'status';
export type SortDirection = 'asc' | 'desc';

export interface SortSpec {
  key: SortKey;
  direction: SortDirection;
}

/**
 * Default — title ascending, matching the server's tie-broken sort.
 */
export const DEFAULT_SORT: SortSpec = { key: 'title', direction: 'asc' };

/**
 * Display order weight for the status sort key. Mirrors the
 * STATUS_PRIORITY in lib/status.ts conceptually but adapted for "what
 * does the user want to see first?" — airing (active interest) before
 * incomplete (action needed) before complete (settled) before
 * untracked / error (administrative).
 */
const STATUS_SORT_ORDER: Record<Status, number> = {
  airing: 0,
  incomplete: 1,
  complete: 2,
  untracked: 3,
  error: 4,
};

function compareTitles(a: string, b: string): number {
  return a.localeCompare(b, undefined, { sensitivity: 'base' });
}

/**
 * Returns the synthetic display-status for a row: `'airing'` when
 * `row.isAiring` is true, otherwise the wire status. Used by the
 * status sort + chip palette so airing series surface at the top of
 * the status sort regardless of their underlying complete /
 * incomplete state.
 */
export function displayStatus(row: ListRow): Status {
  if (row.isAiring) {
    return 'airing';
  }
  return row.status as Status;
}

/**
 * Returns rows whose status is in the active filter set. An empty set
 * is treated as "no filter applied" — all rows pass through.
 *
 * The synthetic `'airing'` chip filters on `row.isAiring` instead of
 * `row.status` (the wire dropped airing as a status; it's now a
 * separate flag). Other chips filter on `row.status` as before. Active
 * `'airing'` ORs with any wire-status chips (a series matches when
 * either condition holds).
 */
export function filterByStatus(
  rows: readonly ListRow[],
  active: ReadonlySet<Status>,
): readonly ListRow[] {
  if (active.size === 0) {
    return rows;
  }
  const wantAiring = active.has('airing');
  return rows.filter((row) => {
    if (wantAiring && row.isAiring) {
      return true;
    }
    return active.has(row.status as Status);
  });
}

/**
 * Generic multi-valued filter — passes rows whose `getter` array
 * intersects the active set. Empty active set short-circuits to the
 * input array (no filter applied).
 */
export function filterByMultiValuedField(
  rows: readonly ListRow[],
  active: ReadonlySet<string>,
  getter: (row: ListRow) => readonly string[] | undefined,
): readonly ListRow[] {
  if (active.size === 0) {
    return rows;
  }
  return rows.filter((row) => {
    const values = getter(row);
    if (!values) {
      return false;
    }
    return values.some((v) => active.has(v));
  });
}

/**
 * Tallies how many rows expose each distinct value of a multi-valued
 * field (`sources`, `resolutions`). A row contributes once per distinct
 * value it carries, even if duplicates appear in its array.
 */
export function countMultiValuedField(
  rows: readonly ListRow[],
  getter: (row: ListRow) => readonly string[] | undefined,
): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const row of rows) {
    const values = getter(row);
    if (!values || values.length === 0) {
      continue;
    }
    const seen = new Set<string>();
    for (const v of values) {
      if (seen.has(v)) {
        continue;
      }
      seen.add(v);
      counts[v] = (counts[v] ?? 0) + 1;
    }
  }
  return counts;
}

/**
 * Returns a new array sorted by the requested column + direction.
 * Title is always the secondary tiebreaker so the order stays stable
 * across re-renders.
 */
export function sortRows(rows: readonly ListRow[], sort: SortSpec): readonly ListRow[] {
  const dir = sort.direction === 'asc' ? 1 : -1;
  const sorted = [...rows];
  sorted.sort((a, b) => {
    let cmp = 0;
    switch (sort.key) {
      case 'title':
        cmp = compareTitles(a.title, b.title);
        break;
      case 'episodes':
        cmp = a.episodesAvailable - b.episodesAvailable;
        if (cmp === 0) {
          cmp = a.episodeCount - b.episodeCount;
        }
        break;
      case 'status':
        cmp = STATUS_SORT_ORDER[displayStatus(a)] - STATUS_SORT_ORDER[displayStatus(b)];
        break;
    }
    if (cmp === 0) {
      cmp = compareTitles(a.title, b.title);
    }
    return cmp * dir;
  });
  return sorted;
}

/**
 * Filters `rows` to those whose `metadataRef` appears in `candidates`,
 * preserving candidate order (so the most relevant matches sort
 * first). Untracked rows have no metadataRef and are excluded by
 * design — the resolve flow speaks the metadata vocabulary, and
 * untracked folders haven't been mapped into it yet.
 */
export function intersectWithCandidates(
  rows: readonly ListRow[],
  candidates: readonly Candidate[],
): readonly ListRow[] {
  if (candidates.length === 0) {
    return [];
  }
  const rank = new Map<string, number>();
  candidates.forEach((c, i) => rank.set(c.ref, i));

  const matched: { row: ListRow; index: number }[] = [];
  for (const row of rows) {
    if (!row.metadataRef) {
      continue;
    }
    const idx = rank.get(row.metadataRef);
    if (idx !== undefined) {
      matched.push({ row, index: idx });
    }
  }
  matched.sort((a, b) => a.index - b.index);
  return matched.map((m) => m.row);
}
