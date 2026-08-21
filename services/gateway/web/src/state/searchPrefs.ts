import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface SearchPrefsStore {
  /** Filter TVDB search candidates down to anime (Settings → Search). */
  animeOnly: boolean;
  setAnimeOnly: (animeOnly: boolean) => void;
}

const SEARCH_PREFS_STORAGE_KEY = 'kura.search-prefs';

/**
 * TVDB search preferences, set from Settings → Search. Kura is
 * anime-first, so the anime-only filter defaults on; the toggle is
 * the escape hatch for the occasional live-action or western lookup.
 *
 * Client-side only: the resolve API always returns the full candidate
 * list (with genres), and the grid filters it locally via
 * `isAnimeCandidate`.
 */
export const useSearchPrefs = create<SearchPrefsStore>()(
  persist(
    (set) => ({
      animeOnly: true,
      setAnimeOnly: (animeOnly) => set({ animeOnly }),
    }),
    { name: SEARCH_PREFS_STORAGE_KEY, version: 1 },
  ),
);
