import type { TrashEmpty, TrashSeriesEntry } from '@/api/types.gen';
import { formatBytesDecimal, formatCompactAge, formatStampUTC } from '@/lib/format';

/**
 * Trash page pure logic: the older-than filter vocabulary, client-side
 * filtering + sorting of the listing, and the mapping from an empty
 * response to a result-banner outcome.
 *
 * Everything here is a pure function so the page component holds only
 * view state and the tests can drive the rules directly.
 */

const HOUR_MS = 3_600_000;

/**
 * Size / age / stamp rendering. The implementations are shared with
 * the releases page and live in `lib/format`; these names are what the
 * trash surface and its tests call them.
 */
export const formatTrashBytes = formatBytesDecimal;
export const formatTrashAge = formatCompactAge;
export const formatTrashStamp = formatStampUTC;

/** Milliseconds since an entry was trashed. `0` when unparseable. */
export function trashEntryAge(trashedAt: string, now: number): number {
  const at = new Date(trashedAt).getTime();
  return Number.isNaN(at) ? 0 : Math.max(0, now - at);
}

/**
 * On-disk footprint of one entry: the media file plus every companion
 * that was trashed with it. Companions are part of what emptying
 * reclaims, so a size that omitted them would understate the action.
 */
export function trashEntryBytes(entry: TrashSeriesEntry['entries'][number]): number {
  return (entry.companions ?? []).reduce((total, c) => total + c.sizeBytes, entry.sizeBytes);
}

/** Footprint of every entry in a series group. */
export function trashGroupBytes(group: TrashSeriesEntry): number {
  return group.entries.reduce((total, entry) => total + trashEntryBytes(entry), 0);
}

/** Footprint across groups. */
export function trashTotalBytes(groups: readonly TrashSeriesEntry[]): number {
  return groups.reduce((total, group) => total + trashGroupBytes(group), 0);
}

/** Entry count across groups. */
export function trashTotalEntries(groups: readonly TrashSeriesEntry[]): number {
  return groups.reduce((total, group) => total + group.entries.length, 0);
}

/** Age of the oldest entry across groups, in ms. `0` when empty. */
export function trashOldestAge(groups: readonly TrashSeriesEntry[], now: number): number {
  let oldest = 0;
  for (const group of groups) {
    for (const entry of group.entries) {
      oldest = Math.max(oldest, trashEntryAge(entry.trashedAt, now));
    }
  }
  return oldest;
}

/** Timestamp (ms) of the most recently trashed entry in a group. */
export function trashGroupNewestAt(group: TrashSeriesEntry): number {
  return group.entries.reduce((newest, entry) => {
    const at = new Date(entry.trashedAt).getTime();
    return Number.isNaN(at) ? newest : Math.max(newest, at);
  }, Number.NEGATIVE_INFINITY);
}

export interface AgeOption {
  /** Minimum entry age in hours. `0` means "no age filter". */
  hours: number;
  label: string;
}

/**
 * The older-than vocabulary, mirroring the CLI's `--older-than`.
 * Expressed in hours because that is the only unit the wire accepts —
 * see `olderThanParam`.
 */
export const TRASH_AGE_OPTIONS: readonly AgeOption[] = [
  { hours: 0, label: 'Any age' },
  { hours: 48, label: '48 hours' },
  { hours: 24 * 7, label: '1 week' },
  { hours: 24 * 30, label: '30 days' },
  { hours: 24 * 90, label: '90 days' },
];

export function trashAgeLabel(hours: number): string {
  return TRASH_AGE_OPTIONS.find((option) => option.hours === hours)?.label ?? 'Any age';
}

/**
 * The `olderThan` query value for a selected age, or `undefined` when
 * no filter is active (the parameter is then omitted entirely).
 *
 * The server parses this with Go's `time.ParseDuration`, whose largest
 * unit is the hour — `7d` is a parse error, not a week. Every option
 * therefore serialises as hours.
 */
export function olderThanParam(hours: number): string | undefined {
  return hours > 0 ? `${hours}h` : undefined;
}

/**
 * Drops entries younger than the selected age, then drops groups the
 * filter emptied. Mirrors the server's `olderThan` semantics (an entry
 * passes once it is *at least* that old) so the listing previews
 * exactly what the scoped empty would delete.
 */
export function filterTrashByAge(
  groups: readonly TrashSeriesEntry[],
  maxAgeHours: number,
  now: number,
): TrashSeriesEntry[] {
  if (maxAgeHours <= 0) {
    return [...groups];
  }
  const floor = maxAgeHours * HOUR_MS;
  return groups
    .map((group) => ({
      ...group,
      entries: group.entries.filter((entry) => trashEntryAge(entry.trashedAt, now) >= floor),
    }))
    .filter((group) => group.entries.length > 0);
}

export type TrashSortKey = 'size' | 'trashed' | 'title';
export type SortDirection = 'asc' | 'desc';

export interface TrashSortSpec {
  key: TrashSortKey;
  direction: SortDirection;
}

export const DEFAULT_TRASH_SORT: TrashSortSpec = { key: 'size', direction: 'desc' };

/**
 * Next sort spec when the user picks `key`. Re-picking the active key
 * flips direction; a new key starts at the direction that is useful
 * first — biggest / most recent for size and trashed, A-Z for title.
 */
export function nextTrashSort(current: TrashSortSpec, key: TrashSortKey): TrashSortSpec {
  if (current.key === key) {
    return { key, direction: current.direction === 'asc' ? 'desc' : 'asc' };
  }
  return { key, direction: key === 'title' ? 'asc' : 'desc' };
}

/**
 * Orders the listing. `trashed` ranks by the group's most recent entry
 * (ascending direction puts the most recently trashed first, matching
 * the reference's age-based ordering). Title is the tiebreak for every
 * key so equal-size groups don't shuffle between renders.
 */
export function sortTrashGroups(
  groups: readonly TrashSeriesEntry[],
  sort: TrashSortSpec,
): TrashSeriesEntry[] {
  const dir = sort.direction === 'asc' ? 1 : -1;
  const byTitle = (a: TrashSeriesEntry, b: TrashSeriesEntry) =>
    a.directory.localeCompare(b.directory, undefined, { sensitivity: 'base' });
  return [...groups].sort((a, b) => {
    let cmp: number;
    if (sort.key === 'size') {
      cmp = trashGroupBytes(a) - trashGroupBytes(b);
    } else if (sort.key === 'trashed') {
      cmp = trashGroupNewestAt(a) - trashGroupNewestAt(b);
    } else {
      cmp = byTitle(a, b);
    }
    if (cmp === 0) {
      cmp = byTitle(a, b);
    }
    return cmp * dir;
  });
}

export type TrashOutcomeKind = 'success' | 'partial' | 'none';

export interface TrashOutcome {
  kind: TrashOutcomeKind;
  entries: number;
  bytes: number;
  failures: { directory: string; error: string }[];
}

/**
 * Maps an empty response onto the three outcomes the banner renders.
 *
 * Failures are checked before `attempts === 0` deliberately: a sweep
 * that attempted nothing *and* reported a failure did not leave the
 * trash untouched because nothing matched — it failed. Reporting that
 * as "nothing matched" is the exact misreading the server's
 * `api.TrashEmpty` doc comment calls out.
 */
export function trashOutcome(result: TrashEmpty): TrashOutcome {
  const failures = result.failures ?? [];
  const shared = {
    entries: result.totalEntries,
    bytes: result.reclaimedBytes,
    failures,
  };
  if (failures.length > 0) {
    return { kind: 'partial', ...shared };
  }
  if ((result.attempts ?? 0) === 0) {
    return { kind: 'none', ...shared };
  }
  return { kind: 'success', ...shared };
}
