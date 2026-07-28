import type { ListStatus } from '@/api/types';

/**
 * Series status — superset of the wire `ListStatus` plus the
 * UI-synthesized `'airing'` value. The wire dropped `'airing'` as a
 * status (it now lives on `ListRow.isAiring`); the UI keeps the
 * existing chip palette by deriving a compound status from
 * `(row.status, row.isAiring)` at render time.
 */
export type Status = ListStatus | 'airing';

/**
 * A single status, or a compound state (e.g. `["airing", "incomplete"]`
 * for a series that's currently airing but missing some past episodes).
 * Wire shape matches the prototype and the future server contract.
 */
export type StatusValue = Status | readonly Status[];

/**
 * Display labels — sentence-case. Keep short; the chip is small.
 */
export const STATUS_LABELS: Record<Status, string> = {
  complete: 'Complete',
  incomplete: 'Incomplete',
  airing: 'Airing',
  untracked: 'Untracked',
  error: 'Error',
};

/**
 * Precedence used when collapsing a compound status to one chip color.
 * Earlier entries win. Rationale: errors always demand attention;
 * "missing episodes" is more actionable than "currently airing"; the
 * benign-but-unmanaged untracked state defers to anything tracked;
 * complete is the resting state.
 */
export const STATUS_PRIORITY: readonly Status[] = [
  'error',
  'incomplete',
  'airing',
  'untracked',
  'complete',
];

function asArray(value: StatusValue): readonly Status[] {
  return Array.isArray(value) ? value : [value as Status];
}

/**
 * Returns the dominant status for display. Falls back to the first
 * entry if nothing in the priority list is present (defensive — the
 * server never emits empty arrays).
 */
export function primaryStatus(value: StatusValue): Status {
  const arr = asArray(value);
  for (const s of STATUS_PRIORITY) {
    if (arr.includes(s)) {
      return s;
    }
  }
  return arr[0] ?? 'complete';
}

/**
 * Returns the secondary status to surface alongside the primary, if
 * the value is compound. Used by chip + poster overlays to show
 * additional context (e.g. an airing dot on top of an incomplete
 * chip).
 */
export function secondaryStatus(value: StatusValue): Status | undefined {
  const arr = asArray(value);
  if (arr.length < 2) {
    return undefined;
  }
  const primary = primaryStatus(value);
  return arr.find((s) => s !== primary);
}

/**
 * withAiring synthesizes a UI status value from a wire `ListStatus` plus
 * the `isAiring` flag. The wire dropped `'airing'` as a status (it now
 * lives in `row.isAiring` / `show.isAiring`); this helper restores the
 * compound display so the existing chip palette keeps working:
 *
 *   - isAiring=false → wire status, single value.
 *   - isAiring=true + status=complete → `'airing'` (single).
 *   - isAiring=true + other status → compound `['airing', status]` so
 *     both the airing dot and the underlying chip surface.
 */
export function withAiring(status: ListStatus, isAiring: boolean): StatusValue {
  if (!isAiring) {
    return status as Status;
  }
  if (status === 'complete') {
    return 'airing';
  }
  return ['airing', status as Status];
}
