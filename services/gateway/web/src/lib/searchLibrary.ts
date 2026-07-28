import Fuse, { type IFuseOptions } from 'fuse.js';

import type { ListRow } from '@/api/types';

/**
 * Per-key weights drive ranking when a query matches multiple
 * fields. Title fields outweigh searchKey because users typically
 * remember canonical / preferred titles; the alias-folded blob is
 * the safety net for romaji shorthands and English alt-titles
 * stitched in by the server. Keep total = 1 so threshold semantics
 * stay readable.
 */
const FUSE_OPTIONS: IFuseOptions<ListRow> = {
  keys: [
    // `title` carries the server-selected display title (preferred-
    // language-resolved). `canonicalTitle` keeps the provider's
    // default-language form for the rare case the two diverge.
    // `searchKey` is the alias-fold safety net for romaji
    // shorthands and translated-title material.
    { name: 'title', weight: 0.5 },
    { name: 'canonicalTitle', weight: 0.3 },
    { name: 'searchKey', weight: 0.2 },
  ],
  // 0.3 tolerates ~1 typo per 3-4 chars while keeping unrelated
  // rows out. Tuned against the typical anime library (hundreds of
  // rows, romaji + JP titles mixed); 0.4 was too permissive.
  threshold: 0.3,
  // Without ignoreLocation, Fuse penalizes matches late in a long
  // string — meaning a token at the tail of the searchKey blob
  // would barely score. We want all tokens equal-distance.
  ignoreLocation: true,
  // 3-char floor: 2-char fragments matched too many CJK rows
  // (any "re", "no", "shi" hit). 3 still lets "ore" → "oreimo".
  minMatchCharLength: 3,
};

/**
 * Build a Fuse index keyed on the row's display titles + the
 * server-folded `searchKey`. Memoize at the call site (typically
 * via `useMemo`) — Fuse construction reads every row once.
 */
export function buildSearchIndex(rows: readonly ListRow[]): Fuse<ListRow> {
  // Fuse mutates its internal collection on add/remove; we always
  // build fresh from a snapshot, so a `readonly` view in is fine.
  return new Fuse(rows as ListRow[], FUSE_OPTIONS);
}

/**
 * Filter `rows` against `query`, returning matches in score order
 * (best first). Empty / whitespace-only queries short-circuit to
 * the full list (caller's existing sort applies).
 *
 * For non-empty queries Fuse owns the ordering — the caller's sort
 * is bypassed for that view. Acceptable because search relevance
 * trumps sort while the user is actively typing.
 */
export function searchLibrary(rows: readonly ListRow[], query: string): readonly ListRow[] {
  const trimmed = query.trim();
  if (!trimmed) {
    return rows;
  }
  const fuse = buildSearchIndex(rows);
  return fuse.search(trimmed).map((result) => result.item);
}
