import { create } from 'zustand';

import { DEFAULT_TRASH_SORT, type TrashSortSpec } from '@/lib/trash';

interface TrashViewStore {
  /** Minimum entry age in hours. `0` = no age filter. */
  maxAgeHours: number;
  sort: TrashSortSpec;
  setMaxAgeHours: (hours: number) => void;
  setSort: (sort: TrashSortSpec) => void;
  clear: () => void;
}

/**
 * Trash page filter + sort state.
 *
 * Deliberately NOT persisted, unlike the library's filters. The
 * library's filters survive a trip into a series and back because that
 * round trip is part of browsing; trash has no such journey, and the
 * shell convention is that re-clicking an active nav item returns the
 * page to its feature default. A persisted age filter would quietly
 * narrow the scope of a destructive action across sessions.
 */
export const useTrashView = create<TrashViewStore>((set) => ({
  maxAgeHours: 0,
  sort: DEFAULT_TRASH_SORT,
  setMaxAgeHours: (maxAgeHours) => set({ maxAgeHours }),
  setSort: (sort) => set({ sort }),
  clear: () => set({ maxAgeHours: 0, sort: DEFAULT_TRASH_SORT }),
}));
