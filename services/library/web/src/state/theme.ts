import { create } from 'zustand';
import { persist } from 'zustand/middleware';

/**
 * Three-way theme selection. `light` and `dark` are explicit user
 * choices; `system` defers to `prefers-color-scheme` and re-applies
 * automatically when the OS toggles. Wire values are stable strings
 * so URL sync / persisted storage stays well-defined.
 */
export type Theme = 'light' | 'dark' | 'system';

/**
 * Palette identifier written to `data-k-theme`. Matches the keys the
 * @theme tokens in globals.css were built around — `paper` for the
 * warm-light surface set, `dark` for navy. Decoupled from `Theme` so
 * adding a new explicit choice (e.g. solarized) doesn't break wire
 * compatibility.
 */
type Palette = 'paper' | 'dark';

interface ThemeStore {
  theme: Theme;
  setTheme: (theme: Theme) => void;
}

const THEME_STORAGE_KEY = 'kura.theme';
const SYSTEM_DARK_QUERY = '(prefers-color-scheme: dark)';

function resolvePalette(theme: Theme): Palette {
  if (theme === 'dark') {
    return 'dark';
  }
  if (theme === 'light') {
    return 'paper';
  }
  if (typeof window === 'undefined' || !window.matchMedia) {
    return 'paper';
  }
  return window.matchMedia(SYSTEM_DARK_QUERY).matches ? 'dark' : 'paper';
}

function applyTheme(theme: Theme): void {
  if (typeof document === 'undefined') {
    return;
  }
  document.documentElement.dataset.kTheme = resolvePalette(theme);
}

/**
 * Theme store. Theme is not a secret — persists to localStorage so the
 * choice survives tab close. Pre-paint sync against the same key
 * happens via an inline script in index.html so the first frame
 * doesn't flash the wrong palette.
 *
 * `system` mode subscribes to OS-level `prefers-color-scheme` changes
 * once on module init so the palette flips when the OS toggles, even
 * with the tab in the background.
 */
export const useTheme = create<ThemeStore>()(
  persist(
    (set) => ({
      theme: 'system',
      setTheme: (theme) => {
        applyTheme(theme);
        set({ theme });
      },
    }),
    {
      name: THEME_STORAGE_KEY,
      version: 1,
      // v0 stored 'paper' (palette key) directly as the theme. Map it
      // to 'light' so the new tri-state semantics line up.
      migrate: (persistedState, version) => {
        if (version === 0 && persistedState && typeof persistedState === 'object') {
          const old = persistedState as { theme?: string };
          if (old.theme === 'paper') {
            return { ...old, theme: 'light' };
          }
          if (old.theme === 'dark' || old.theme === 'light' || old.theme === 'system') {
            return persistedState;
          }
          return { ...old, theme: 'system' };
        }
        return persistedState;
      },
      onRehydrateStorage: () => (state) => {
        if (state) {
          applyTheme(state.theme);
        }
      },
    },
  ),
);

if (typeof window !== 'undefined' && window.matchMedia) {
  // System-mode listener — applies the resolved palette whenever the
  // OS flips dark / light. No-op when the user's explicit choice is
  // light or dark; the matchMedia event still fires but applyTheme
  // sees the locked theme and writes the right palette anyway.
  const mq = window.matchMedia(SYSTEM_DARK_QUERY);
  mq.addEventListener('change', () => {
    const { theme } = useTheme.getState();
    if (theme === 'system') {
      applyTheme('system');
    }
  });
}
