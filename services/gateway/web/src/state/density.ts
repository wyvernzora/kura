import { create } from 'zustand';
import { persist } from 'zustand/middleware';

import type { DensityPreference } from '@/lib/useAutoDensity';

interface DensityStore {
  preference: DensityPreference;
  setPreference: (preference: DensityPreference) => void;
}

const DENSITY_STORAGE_KEY = 'kura.density';

/**
 * Poster-grid density preference, set from Settings → Appearance.
 * `comfortable` is the auto-sized grid the viewport picks on its own;
 * `compact` shifts that pick one bucket smaller so more posters fit
 * per row (see `applyDensityPreference`).
 *
 * Persists to localStorage for the same reason the theme does — it is
 * a durable display preference, not session state.
 */
export const useDensityPreference = create<DensityStore>()(
  persist(
    (set) => ({
      preference: 'comfortable',
      setPreference: (preference) => set({ preference }),
    }),
    { name: DENSITY_STORAGE_KEY, version: 1 },
  ),
);
