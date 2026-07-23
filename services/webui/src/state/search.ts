import { create } from 'zustand';

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
  setQuery: (query: string) => void;
  setOrigin: (origin: string | null) => void;
  clear: () => void;
}

/**
 * Search query store. Shared between the top-bar SearchField (writer)
 * and the library home (reader → useResolveSearch). One value, two
 * consumers — small enough that a Context would also work, but
 * Zustand keeps the read in components without a Provider.
 */
export const useSearch = create<SearchStore>((set) => ({
  query: '',
  origin: null,
  setQuery: (query) => set({ query }),
  setOrigin: (origin) => set({ origin }),
  clear: () => set({ query: '', origin: null }),
}));
