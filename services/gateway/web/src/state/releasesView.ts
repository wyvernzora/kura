import { create } from 'zustand';

import { ATTENTION_STATUSES } from '@/lib/releases';
import type { ReleaseFilterKey } from '@/state/releaseStreams';

/**
 * The default view: the attention set. Exhausted releases plus the
 * low-confidence pseudo-filter — the two states actually asking the
 * operator for a decision.
 */
export const ATTENTION_FILTER: readonly ReleaseFilterKey[] = [...ATTENTION_STATUSES, 'lowconf'];

interface ReleasesViewStore {
  active: ReadonlySet<ReleaseFilterKey>;
  /**
   * Infohash of the open release detail; null = the queue listing.
   * Lives here rather than in ReleasesPage state because the shell's
   * mobile page bar renders the detail's back button, and the shell
   * owns that bar.
   */
  openInfohash: string | null;
  openRelease: (infohash: string) => void;
  closeRelease: () => void;
  toggle: (key: ReleaseFilterKey) => void;
  /** Empty set = every status, the menu's "All". */
  showAll: () => void;
  /** Back to the feature default, the way re-clicking the nav item does. */
  clear: () => void;
}

/**
 * Release-attention filter state.
 *
 * Not persisted, following the trash page: re-clicking an active nav
 * item returns a page to its feature default, and an attention queue
 * that silently opened pre-narrowed would hide work rather than
 * surface it.
 */
export const useReleasesView = create<ReleasesViewStore>((set) => ({
  active: new Set(ATTENTION_FILTER),
  openInfohash: null,
  openRelease: (infohash) => set({ openInfohash: infohash }),
  closeRelease: () => set({ openInfohash: null }),
  toggle: (key) =>
    set((s) => {
      const next = new Set(s.active);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return { active: next };
    }),
  showAll: () => set({ active: new Set() }),
  clear: () => set({ active: new Set(ATTENTION_FILTER), openInfohash: null }),
}));
