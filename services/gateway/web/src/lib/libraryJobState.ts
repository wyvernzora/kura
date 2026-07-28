/**
 * Persistent client-side state for the single library-wide maintenance
 * job (scan-all or reindex). One record per browser, keyed under
 * `kura.libraryJob` — both kinds share the slot because the backend
 * registry only allows one library-wide job at a time (zero-series
 * cross-kind busy rejection in internal/jobs).
 *
 * Unlike per-series scan state, terminal warn/error are NOT persisted
 * here. Library jobs are coarse maintenance — on terminal we clear
 * the record so the gear returns to idle. The hook surfaces a
 * transient `lastError` for one render after a failure so the UI can
 * flash a banner.
 */
export type LibraryJobKind = 'scan' | 'reindex';

export type LibraryJobRecord = {
  state: 'running';
  kind: LibraryJobKind;
  jobId: string;
  startedAt: string;
};

const KEY = 'kura.libraryJob';

export function readLibraryJobRecord(): LibraryJobRecord | undefined {
  if (typeof window === 'undefined') {
    return undefined;
  }
  const raw = window.localStorage.getItem(KEY);
  if (!raw) {
    return undefined;
  }
  try {
    return JSON.parse(raw) as LibraryJobRecord;
  } catch {
    window.localStorage.removeItem(KEY);
    return undefined;
  }
}

export function writeLibraryJobRecord(record: LibraryJobRecord): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(KEY, JSON.stringify(record));
}

export function clearLibraryJobRecord(): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.removeItem(KEY);
}

/**
 * Cross-tab subscriber. Fires whenever any tab writes or clears the
 * library-job key. Returns unsubscribe.
 */
export function subscribeLibraryJobRecord(
  onChange: (record: LibraryJobRecord | undefined) => void,
): () => void {
  if (typeof window === 'undefined') {
    return () => {};
  }
  function handler(ev: StorageEvent) {
    if (ev.key !== KEY) {
      return;
    }
    if (!ev.newValue) {
      onChange(undefined);
      return;
    }
    try {
      onChange(JSON.parse(ev.newValue) as LibraryJobRecord);
    } catch {
      onChange(undefined);
    }
  }
  window.addEventListener('storage', handler);
  return () => window.removeEventListener('storage', handler);
}
