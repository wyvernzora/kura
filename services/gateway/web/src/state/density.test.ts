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

import { useDensityPreference } from './density';

function persisted(): { preference: string } | null {
  const raw = entries.get('kura.density');
  return raw ? (JSON.parse(raw).state as { preference: string }) : null;
}

describe('density preference store', () => {
  beforeEach(() => {
    entries.clear();
    useDensityPreference.setState({ preference: 'comfortable' });
  });

  it('defaults to comfortable', () => {
    expect(useDensityPreference.getInitialState().preference).toBe('comfortable');
  });

  it('setPreference switches to compact', () => {
    useDensityPreference.getState().setPreference('compact');
    expect(useDensityPreference.getState().preference).toBe('compact');
  });

  it('persists the choice under kura.density', () => {
    useDensityPreference.getState().setPreference('compact');
    expect(persisted()).toEqual({ preference: 'compact' });
  });
});
