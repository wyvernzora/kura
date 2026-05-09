import type { JobError, ScanSkip } from '@/api/types.gen';

/**
 * Persistent client-side scan state, keyed by series metadata ref. Three
 * variants:
 *
 *   running   — scan submitted; a SSE stream is (or should be) attached
 *   warning   — terminal success with skipped files; persists until next scan
 *   error     — terminal failure; persists until next scan
 *
 * Successful clean runs do not produce a record — the hook clears the
 * key on terminal success so the UI returns to the idle state with the
 * fresh `lastScanned` timestamp from the Show fetch.
 */
export type ScanRecord =
  | { state: 'running'; jobId: string; startedAt: string }
  | {
      state: 'warning';
      jobId: string;
      skipped: ScanSkip[];
      finishedAt: string;
    }
  | {
      state: 'error';
      jobId: string;
      error: JobError;
      progressFrozen: number;
      finishedAt: string;
    };

const PREFIX = 'kura.scan.';

function key(metadataRef: string): string {
  return PREFIX + metadataRef;
}

export function readScanRecord(metadataRef: string): ScanRecord | undefined {
  if (typeof window === 'undefined') {
    return undefined;
  }
  const raw = window.localStorage.getItem(key(metadataRef));
  if (!raw) {
    return undefined;
  }
  try {
    return JSON.parse(raw) as ScanRecord;
  } catch {
    window.localStorage.removeItem(key(metadataRef));
    return undefined;
  }
}

export function writeScanRecord(metadataRef: string, record: ScanRecord): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(key(metadataRef), JSON.stringify(record));
}

export function clearScanRecord(metadataRef: string): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.removeItem(key(metadataRef));
}

/**
 * Subscribe to cross-tab updates for one series's scan record. Fires
 * whenever another tab writes or clears the key. Returns the unsubscribe
 * fn so the caller can wire this into a useEffect cleanup.
 */
export function subscribeScanRecord(
  metadataRef: string,
  onChange: (record: ScanRecord | undefined) => void,
): () => void {
  if (typeof window === 'undefined') {
    return () => {};
  }
  const k = key(metadataRef);
  function handler(ev: StorageEvent) {
    if (ev.key !== k) {
      return;
    }
    if (!ev.newValue) {
      onChange(undefined);
      return;
    }
    try {
      onChange(JSON.parse(ev.newValue) as ScanRecord);
    } catch {
      onChange(undefined);
    }
  }
  window.addEventListener('storage', handler);
  return () => window.removeEventListener('storage', handler);
}
