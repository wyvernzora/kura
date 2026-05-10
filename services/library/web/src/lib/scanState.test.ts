import { afterEach, beforeAll, describe, expect, it } from 'vitest';

import {
  clearScanRecord,
  readScanRecord,
  type ScanRecord,
  subscribeScanRecord,
  writeScanRecord,
} from './scanState';

const REF = 'tvdb:12345';

// Node 25's experimental localStorage is broken without `--localstorage-file`,
// and full DOM environments (jsdom / happy-dom) inherit that brokenness when
// vitest runs them under recent Node. Stub the small surface scanState
// touches: localStorage CRUD and `window.addEventListener('storage', ...)`.
const storage = new Map<string, string>();
const listeners = new Set<(e: StorageEvent) => void>();

beforeAll(() => {
  const fakeLocalStorage = {
    getItem: (k: string) => storage.get(k) ?? null,
    setItem: (k: string, v: string) => {
      storage.set(k, v);
    },
    removeItem: (k: string) => {
      storage.delete(k);
    },
    clear: () => {
      storage.clear();
    },
    key: (i: number) => Array.from(storage.keys())[i] ?? null,
    get length() {
      return storage.size;
    },
  };
  const fakeWindow = {
    localStorage: fakeLocalStorage,
    addEventListener: (type: string, fn: (e: StorageEvent) => void) => {
      if (type === 'storage') {
        listeners.add(fn);
      }
    },
    removeEventListener: (type: string, fn: (e: StorageEvent) => void) => {
      if (type === 'storage') {
        listeners.delete(fn);
      }
    },
  };
  // biome-ignore lint/suspicious/noExplicitAny: test stub
  (globalThis as any).window = fakeWindow;
});

function fireStorage(ev: { key: string; newValue: string | null }) {
  for (const fn of listeners) {
    fn(ev as unknown as StorageEvent);
  }
}

afterEach(() => {
  storage.clear();
  listeners.clear();
});

describe('scanState', () => {
  it('returns undefined when no record exists', () => {
    expect(readScanRecord(REF)).toBeUndefined();
  });

  it('round-trips a running record', () => {
    const rec: ScanRecord = {
      state: 'running',
      jobId: 'abc',
      startedAt: '2026-05-09T10:00:00Z',
    };
    writeScanRecord(REF, rec);
    expect(readScanRecord(REF)).toEqual(rec);
  });

  it('round-trips a warning record with skipped files', () => {
    const rec: ScanRecord = {
      state: 'warning',
      jobId: 'abc',
      finishedAt: '2026-05-09T10:01:00Z',
      skipped: [{ path: 'Season 01/x.mkv', code: 'season_mismatch', reason: 'mismatch' }],
    };
    writeScanRecord(REF, rec);
    expect(readScanRecord(REF)).toEqual(rec);
  });

  it('clears a record', () => {
    writeScanRecord(REF, {
      state: 'running',
      jobId: 'abc',
      startedAt: '2026-05-09T10:00:00Z',
    });
    clearScanRecord(REF);
    expect(readScanRecord(REF)).toBeUndefined();
  });

  it('treats malformed JSON as missing and prunes the key', () => {
    storage.set(`kura.scan.${REF}`, '{not valid json');
    expect(readScanRecord(REF)).toBeUndefined();
    expect(storage.has(`kura.scan.${REF}`)).toBe(false);
  });

  it('partitions records by metadata ref', () => {
    writeScanRecord('tvdb:1', {
      state: 'running',
      jobId: 'a',
      startedAt: '2026-05-09T10:00:00Z',
    });
    writeScanRecord('tvdb:2', {
      state: 'error',
      jobId: 'b',
      finishedAt: '2026-05-09T10:00:00Z',
      progressFrozen: 0.5,
      error: { kind: 'internal', message: 'boom' },
    });
    expect(readScanRecord('tvdb:1')?.state).toBe('running');
    expect(readScanRecord('tvdb:2')?.state).toBe('error');
  });

  it('cross-tab subscriber fires on storage events for the matched key', () => {
    const calls: Array<ScanRecord | undefined> = [];
    const unsubscribe = subscribeScanRecord(REF, (r) => calls.push(r));

    const rec: ScanRecord = {
      state: 'running',
      jobId: 'abc',
      startedAt: '2026-05-09T10:00:00Z',
    };
    fireStorage({ key: `kura.scan.${REF}`, newValue: JSON.stringify(rec) });
    expect(calls.at(-1)).toEqual(rec);

    fireStorage({ key: `kura.scan.${REF}`, newValue: null });
    expect(calls.at(-1)).toBeUndefined();

    const before = calls.length;
    fireStorage({ key: 'kura.scan.other', newValue: 'x' });
    expect(calls.length).toBe(before);

    unsubscribe();
  });
});
