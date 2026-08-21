import type { TrashEmpty, TrashSeriesEntry } from '@/api/types.gen';

/**
 * Trash page pure logic: byte / age / stamp formatting, the older-than
 * filter vocabulary, client-side filtering + sorting of the listing,
 * and the mapping from an empty response to a result-banner outcome.
 *
 * Everything here is a pure function so the page component holds only
 * view state and the tests can drive the rules directly.
 */

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

/**
 * Decimal byte units, matching the CLI's rendering — a 1 TB disk is
 * sold as 1 TB, and trash sizes are read against disk capacity.
 * Sub-GB values round to whole units (a trashed episode is never
 * interesting at 0.1 MB resolution); GB and up keep one decimal until
 * they reach three digits.
 */
export function formatTrashBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit += 1;
  }
  const rendered =
    unit >= 2
      ? value >= 100
        ? String(Math.round(value))
        : value.toFixed(1)
      : String(Math.round(value));
  return `${rendered} ${units[unit]}`;
}

/**
 * Coarse elapsed-time label for trash ages: `m` under an hour, `h`
 * under a day, `d` under two months, `mo` beyond. Never renders `0m` —
 * a just-trashed entry reads as `1m`, since "0 minutes old" invites
 * the reading that the timestamp is missing.
 */
export function formatTrashAge(elapsedMs: number): string {
  if (elapsedMs < HOUR_MS) {
    return `${Math.max(1, Math.round(elapsedMs / 60_000))}m`;
  }
  if (elapsedMs < DAY_MS) {
    return `${Math.round(elapsedMs / HOUR_MS)}h`;
  }
  if (elapsedMs < 60 * DAY_MS) {
    return `${Math.round(elapsedMs / DAY_MS)}d`;
  }
  return `${Math.round(elapsedMs / (30 * DAY_MS))}mo`;
}

/**
 * Compact `YYYY-MM-DD HH:MM` stamp, rendered in UTC. UTC rather than
 * the viewer's zone so the string is stable across machines (the unit
 * tests and the Storybook fixtures both depend on that) and so a
 * server timestamp carrying an offset doesn't silently render in a
 * third zone. The relative age beside it carries the "how long ago"
 * reading, which is the actionable half.
 *
 * Returns an empty string for missing / unparseable input so callers
 * can render nothing without checking.
 */
export function formatTrashStamp(iso: string | undefined): string {
  if (!iso) {
    return '';
  }
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) {
    return '';
  }
  const pad = (n: number) => String(n).padStart(2, '0');
  const date = `${at.getUTCFullYear()}-${pad(at.getUTCMonth() + 1)}-${pad(at.getUTCDate())}`;
  return `${date} ${pad(at.getUTCHours())}:${pad(at.getUTCMinutes())}`;
}

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
