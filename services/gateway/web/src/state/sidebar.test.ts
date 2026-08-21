import { beforeEach, describe, expect, it, vi } from 'vitest';

// zustand's persist middleware reads `window.localStorage` when the
// store module initializes, so the stub has to exist before the
// import below runs — `vi.hoisted` is what gets us in front of it.
// Without it the store still works but silently stops persisting,
// which is exactly what these tests are here to catch.
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

import { useSidebar } from './sidebar';

function persisted(): { collapsed: boolean } | null {
  const raw = entries.get('kura.sidebar.collapsed');
  return raw ? (JSON.parse(raw).state as { collapsed: boolean }) : null;
}

describe('sidebar store', () => {
  beforeEach(() => {
    entries.clear();
    useSidebar.setState({ collapsed: true });
  });

  it('defaults to the collapsed icon rail', () => {
    expect(useSidebar.getInitialState().collapsed).toBe(true);
  });

  it('toggle flips collapsed', () => {
    useSidebar.getState().toggle();
    expect(useSidebar.getState().collapsed).toBe(false);
    useSidebar.getState().toggle();
    expect(useSidebar.getState().collapsed).toBe(true);
  });

  it('persists the toggle under kura.sidebar.collapsed', () => {
    useSidebar.getState().toggle();
    expect(persisted()).toEqual({ collapsed: false });
  });
});
