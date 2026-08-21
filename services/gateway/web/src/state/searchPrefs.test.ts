import { beforeEach, describe, expect, it, vi } from 'vitest';

// See sidebar.test.ts: the persist middleware reads
// `window.localStorage` at module-init time, so the stub is installed
// from a hoisted block that runs before the import below.
const entries = vi.hoisted(() => {
  const map = new Map<string, string>();
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: {
      localStorage: {
        getItem: (k: string) => map.get(k) ?? null,
        setItem: (k: string, v: string) => {
          map.set(k, v);
        },
        removeItem: (k: string) => {
          map.delete(k);
        },
      },
    },
  });
  return map;
});

import { useSearchPrefs } from './searchPrefs';

function persisted(): { animeOnly: boolean } | null {
  const raw = entries.get('kura.search-prefs');
  return raw ? (JSON.parse(raw).state as { animeOnly: boolean }) : null;
}

describe('search prefs store', () => {
  beforeEach(() => {
    entries.clear();
    useSearchPrefs.setState({ animeOnly: true });
  });

  it('defaults to anime-only', () => {
    expect(useSearchPrefs.getInitialState().animeOnly).toBe(true);
  });

  it('setAnimeOnly turns the filter off', () => {
    useSearchPrefs.getState().setAnimeOnly(false);
    expect(useSearchPrefs.getState().animeOnly).toBe(false);
  });

  it('persists under kura.search-prefs', () => {
    useSearchPrefs.getState().setAnimeOnly(false);
    expect(persisted()).toEqual({ animeOnly: false });
  });
});
