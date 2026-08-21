import type { MatchStatus } from '@/api/releaseTypes';
import type { ListRow } from '@/api/types';

/**
 * Release-attention pure logic: the status vocabulary, the confidence
 * ladder, the low-confidence rule, and the per-state action set.
 *
 * The indexer holds no matching policy, so the low-confidence
 * threshold is SPA policy and lives here — it reaches the server only
 * as the `maxConfidence` list parameter, never as a stored setting.
 */

/** Custom window event the shell's mobile page bar fires. */
export const RELEASES_REFRESH_EVENT = 'kura-releases-refresh';

/** Rows added per "Load more" press, and the list page size. */
export const RELEASE_PAGE_SIZE = 8;

/**
 * Auto-matches below this confidence still want an operator eye:
 * affirm the match (operator matches are certain) or hand match to
 * correct it. Client-side policy — the indexer stores no threshold.
 */
export const LOW_CONFIDENCE = 0.75;

export const MATCH_STATUSES: readonly MatchStatus[] = [
  'suppressed',
  'exhausted',
  'unmatched',
  'matched',
  'dead',
];

export const STATUS_LABELS: Record<MatchStatus, string> = {
  unmatched: 'Unmatched',
  matched: 'Matched',
  suppressed: 'Suppressed',
  exhausted: 'Exhausted',
  dead: 'Dead',
};

export const STATUS_DOT: Record<MatchStatus, string> = {
  unmatched: 'bg-status-airing',
  matched: 'bg-status-complete',
  suppressed: 'bg-status-incomplete',
  exhausted: 'bg-status-error',
  dead: 'bg-status-untracked',
};

/**
 * Only `exhausted` is attention on its own. Suppressed is not
 * actionable — the operator may inspect it, and the only way out is a
 * hand match — and `dead` is downloader-owned. Low-confidence matches
 * join through the `lowconf` pseudo-filter.
 */
export const ATTENTION_STATUSES: readonly MatchStatus[] = ['exhausted'];

/**
 * Ladder for the row's confidence number, skewed right because agents
 * discard anything under ~50% rather than matching it:
 *   <52 red · 52-75 orange · 76-89 yellow · >=90 green
 * The orange is interpolated from the palette's error / incomplete
 * pair so it sits in the same family.
 */
const CONFIDENCE_SCALE: readonly { min: number; color: string }[] = [
  { min: 0.9, color: 'var(--color-status-complete)' },
  { min: 0.76, color: 'var(--color-status-incomplete)' },
  { min: 0.52, color: '#d97b32' },
  { min: 0, color: 'var(--color-status-error)' },
];

/**
 * Colour for a confidence score, or `undefined` when there is none —
 * absent confidence renders dimmed, never as a zero-scored match.
 */
export function confidenceColor(confidence: number | null | undefined): string | undefined {
  if (confidence == null) {
    return undefined;
  }
  return CONFIDENCE_SCALE.find((step) => confidence >= step.min)?.color;
}

/** Confidence as a whole-percent label, or `null` when unscored. */
export function confidencePercent(confidence: number | null | undefined): string | null {
  return confidence == null ? null : `${Math.round(confidence * 100)}%`;
}

/**
 * A matched release the matcher was not sure about. Only `matched`
 * qualifies: an unmatched release carries no ref to affirm, and a
 * suppressed one is already its own filter entry.
 */
export function isLowConfidence(
  status: MatchStatus,
  confidence: number | null | undefined,
): boolean {
  return status === 'matched' && confidence != null && confidence < LOW_CONFIDENCE;
}

export type ReleaseActionId = 'match' | 'affirm' | 'requeue';

export interface ReleaseAction {
  id: ReleaseActionId;
  icon: string;
  label: string;
  primary?: boolean;
}

/**
 * What the operator may do to a release in this state.
 *
 *   exhausted        Hand match (primary) · Requeue
 *   suppressed       Hand match (primary)
 *   matched, low     Affirm match (primary) · Correct match
 *   matched          Correct match
 *   unmatched, dead  none
 *
 * `unmatched` is still in the claim queue and `dead` is terminal and
 * downloader-owned, so neither offers an operator transition.
 */
export function releaseActions(
  status: MatchStatus,
  confidence: number | null | undefined,
): ReleaseAction[] {
  const low = isLowConfidence(status, confidence);
  if (status === 'exhausted') {
    return [
      { id: 'match', icon: 'link', label: 'Hand match', primary: true },
      { id: 'requeue', icon: 'restart_alt', label: 'Requeue' },
    ];
  }
  if (status === 'suppressed') {
    return [{ id: 'match', icon: 'link', label: 'Hand match', primary: true }];
  }
  if (status === 'matched') {
    const actions: ReleaseAction[] = [];
    if (low) {
      actions.push({ id: 'affirm', icon: 'verified', label: 'Affirm match', primary: true });
    }
    actions.push({ id: 'match', icon: 'link', label: 'Correct match', primary: !low });
    return actions;
  }
  return [];
}

/**
 * The series half of a canonical ref: `tvdb:424536#s01e28` →
 * `tvdb:424536`. Refs are opaque to the indexer, so the fragment split
 * is the SPA's own reading of the convention.
 */
export function seriesRefOf(ref: string | null | undefined): string {
  if (!ref) {
    return '';
  }
  const hash = ref.indexOf('#');
  return hash === -1 ? ref : ref.slice(0, hash);
}

/**
 * Series title for a release ref, joined client-side against the
 * loaded library list. The indexer never learns about the library, so
 * this lookup is the only thing that turns a ref into a name the
 * operator recognises. Returns `undefined` when the ref is absent or
 * names a series that is not in the library.
 */
export function seriesTitleForRef(
  titles: ReadonlyMap<string, string>,
  ref: string | null | undefined,
): string | undefined {
  const series = seriesRefOf(ref);
  return series ? titles.get(series) : undefined;
}

/** Ref → display title index over the loaded library rows. */
export function seriesTitleIndex(rows: readonly ListRow[]): Map<string, string> {
  const index = new Map<string, string>();
  for (const row of rows) {
    if (row.ref) {
      index.set(row.ref, row.title);
    }
  }
  return index;
}
