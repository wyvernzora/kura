import { create } from 'zustand';

import { DEFAULT_SORT, type SortSpec } from '@/lib/library';
import type { Status } from '@/lib/status';

interface LibraryStore {
  status: ReadonlySet<Status>;
  sources: ReadonlySet<string>;
  resolutions: ReadonlySet<string>;
  sort: SortSpec;
  toggleStatus: (value: Status) => void;
  toggleSource: (value: string) => void;
  toggleResolution: (value: string) => void;
  setSort: (sort: SortSpec) => void;
  clear: () => void;
}

function toggleSet<T>(prev: ReadonlySet<T>, value: T): Set<T> {
  const next = new Set(prev);
  if (next.has(value)) {
    next.delete(value);
  } else {
    next.add(value);
  }
  return next;
}

/**
 * Library home filter + sort state. Lives in a store (not local
 * `useState` inside LibraryHome) so it survives navigation to
 * `/series/$ref` and back — the user comes back to the same filters,
 * the same sort, and (paired with the router's scroll restoration)
 * the same scroll offset. `clear` resets to "first-visit" state and
 * is also wired to the logo click.
 */
export const useLibraryFilters = create<LibraryStore>((set) => ({
  status: new Set(),
  sources: new Set(),
  resolutions: new Set(),
  sort: DEFAULT_SORT,
  toggleStatus: (value) => set((s) => ({ status: toggleSet(s.status, value) })),
  toggleSource: (value) => set((s) => ({ sources: toggleSet(s.sources, value) })),
  toggleResolution: (value) => set((s) => ({ resolutions: toggleSet(s.resolutions, value) })),
  setSort: (sort) => set({ sort }),
  clear: () =>
    set({
      status: new Set(),
      sources: new Set(),
      resolutions: new Set(),
      sort: DEFAULT_SORT,
    }),
}));
