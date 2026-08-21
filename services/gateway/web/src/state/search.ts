import { create } from 'zustand';

/**
 * Results view during a search session. `library` is local fuzzy
 * matches; `tvdb` is provider candidates not in the library.
 */
export type SearchScope = 'library' | 'tvdb';

/**
 * Minimum query length before the TVDB scope becomes available (and
 * before the provider resolve fires). Higher than the resolve hook's
 * own floor (2) so provider search only fires on reasonably specific
 * queries.
 */
export const MIN_TVDB_QUERY_LENGTH = 3;

interface SearchStore {
  query: string;
  /**
   * Path the user was on when they started this search session.
   * Captured on the empty → non-empty transition while NOT on `/`.
   * Used by TopBar to navigate back when the user clears the input
   * while still on the home/results view, so a search initiated from
   * a series detail page rewinds to that page on clear.
   */
  origin: string | null;
  /**
   * Active results scope. Lives here (not component state) because
   * TopBar renders the control and the home route renders the
   * results. Forced back to 'library' whenever the query drops below
   * the TVDB floor — the TVDB segment is disabled there, so the
   * scope must not be able to point at it.
   */
  scope: SearchScope;
  setQuery: (query: string) => void;
  setOrigin: (origin: string | null) => void;
  setScope: (scope: SearchScope) => void;
  clear: () => void;
}

/**
 * Search session store. Shared between the top-bar SearchField +
 * scope control (writers) and the library home (reader →
 * useResolveSearch). Small enough that a Context would also work,
 * but Zustand keeps the read in components without a Provider.
 */
export const useSearch = create<SearchStore>((set) => ({
  query: '',
  origin: null,
  scope: 'library',
  setQuery: (query) =>
    set((state) => ({
      query,
      scope: query.trim().length < MIN_TVDB_QUERY_LENGTH ? 'library' : state.scope,
    })),
  setOrigin: (origin) => set({ origin }),
  setScope: (scope) => set({ scope }),
  clear: () => set({ query: '', origin: null, scope: 'library' }),
}));
