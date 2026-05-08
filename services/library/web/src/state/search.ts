import { create } from 'zustand';

interface SearchStore {
  query: string;
  setQuery: (query: string) => void;
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
  setQuery: (query) => set({ query }),
  clear: () => set({ query: '' }),
}));
