import { afterEach, beforeAll, describe, expect, it } from 'vitest';

import {
  clearLibraryJobRecord,
  type LibraryJobRecord,
  readLibraryJobRecord,
  subscribeLibraryJobRecord,
  writeLibraryJobRecord,
} from './libraryJobState';

const KEY = 'kura.libraryJob';

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

describe('libraryJobState', () => {
  it('returns undefined when no record exists', () => {
    expect(readLibraryJobRecord()).toBeUndefined();
  });

  it('round-trips a running scan record', () => {
    const rec: LibraryJobRecord = {
      state: 'running',
      kind: 'scan',
      jobId: 'abc',
      startedAt: '2026-05-09T10:00:00Z',
    };
    writeLibraryJobRecord(rec);
    expect(readLibraryJobRecord()).toEqual(rec);
  });

  it('round-trips a running reindex record', () => {
    const rec: LibraryJobRecord = {
      state: 'running',
      kind: 'reindex',
      jobId: 'def',
      startedAt: '2026-05-09T10:05:00Z',
    };
    writeLibraryJobRecord(rec);
    expect(readLibraryJobRecord()).toEqual(rec);
  });

  it('clears a record', () => {
    writeLibraryJobRecord({
      state: 'running',
      kind: 'scan',
      jobId: 'abc',
      startedAt: '2026-05-09T10:00:00Z',
    });
    clearLibraryJobRecord();
    expect(readLibraryJobRecord()).toBeUndefined();
  });

  it('treats malformed JSON as missing and prunes the key', () => {
    storage.set(KEY, '{not valid json');
    expect(readLibraryJobRecord()).toBeUndefined();
    expect(storage.has(KEY)).toBe(false);
  });

  it('cross-tab subscriber fires on storage events for the library-job key', () => {
    const calls: Array<LibraryJobRecord | undefined> = [];
    const unsubscribe = subscribeLibraryJobRecord((r) => calls.push(r));

    const rec: LibraryJobRecord = {
      state: 'running',
      kind: 'reindex',
      jobId: 'abc',
      startedAt: '2026-05-09T10:00:00Z',
    };
    fireStorage({ key: KEY, newValue: JSON.stringify(rec) });
    expect(calls.at(-1)).toEqual(rec);

    fireStorage({ key: KEY, newValue: null });
    expect(calls.at(-1)).toBeUndefined();

    const before = calls.length;
    fireStorage({ key: 'kura.scan.tvdb:1', newValue: 'x' });
    expect(calls.length).toBe(before);

    unsubscribe();
  });
});
