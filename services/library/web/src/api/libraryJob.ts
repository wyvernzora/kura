import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';

import { KuraApiError, api } from '@/api/client';
import type { JobStatus } from '@/api/types.gen';
import {
  type LibraryJobKind,
  type LibraryJobRecord,
  clearLibraryJobRecord,
  readLibraryJobRecord,
  subscribeLibraryJobRecord,
  writeLibraryJobRecord,
} from '@/lib/libraryJobState';

const POLL_INTERVAL_MS = 500;
const ERROR_LINGER_MS = 5000;

/**
 * `useLibraryJob` drives the gear menu's library-wide maintenance
 * state machine. One running job at a time (scan-all OR reindex);
 * cross-kind submissions hit the registry's busy rejection path on
 * the server, which the kickoff branch surfaces as a transient
 * `lastError`.
 *
 * Polling, not SSE — same reason as scanJob: browser EventSource
 * cannot attach the Authorization header.
 */
export interface LibraryJobState {
  phase: 'idle' | 'running';
  kind?: LibraryJobKind;
  progress?: { current: number; total: number; message: string };
  jobId?: string;
  /** Most recent terminal failure or kickoff failure, cleared after a short linger. */
  lastError?: { message: string };
  startScan: () => void;
  startReindex: () => void;
}

export function useLibraryJob(): LibraryJobState {
  const queryClient = useQueryClient();
  const [record, setRecord] = useState<LibraryJobRecord | undefined>(() => readLibraryJobRecord());
  const [progress, setProgress] = useState<JobStatus['progress'] | undefined>(undefined);
  const [lastError, setLastError] = useState<{ message: string } | undefined>(undefined);
  const errorTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // Cross-tab: another tab kicked off / completed the library job.
  useEffect(() => {
    return subscribeLibraryJobRecord((next) => {
      setRecord(next);
      if (!next) {
        setProgress(undefined);
      }
    });
  }, []);

  useEffect(() => {
    return () => {
      if (errorTimerRef.current) {
        clearTimeout(errorTimerRef.current);
      }
    };
  }, []);

  const flashError = useCallback((message: string) => {
    setLastError({ message });
    if (errorTimerRef.current) {
      clearTimeout(errorTimerRef.current);
    }
    errorTimerRef.current = setTimeout(() => setLastError(undefined), ERROR_LINGER_MS);
  }, []);

  // Poll job status while running. Mirror of scanJob's polling loop.
  useEffect(() => {
    if (!record || record.state !== 'running') {
      return;
    }
    const jobId = record.jobId;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    async function tick() {
      if (cancelled) {
        return;
      }
      try {
        const status = await api<JobStatus>(`/api/v1/jobs/${encodeURIComponent(jobId)}`);
        if (cancelled) {
          return;
        }
        if (status.progress) {
          setProgress(status.progress);
        }
        if (status.state === 'succeeded') {
          handleTerminal(false, undefined);
          return;
        }
        if (status.state === 'failed') {
          const message = status.error?.message ?? 'Library job failed';
          handleTerminal(true, message);
          return;
        }
      } catch (err) {
        if (cancelled) {
          return;
        }
        if (err instanceof KuraApiError && err.status === 404) {
          // Registry forgot the job — treat as terminal-success-ish: clear
          // the record and let the user retry. Don't flash an error;
          // server may have legitimately restarted.
          handleTerminal(false, undefined);
          return;
        }
        // Transient blip — keep polling.
      }
      timer = setTimeout(tick, POLL_INTERVAL_MS);
    }

    function handleTerminal(failed: boolean, errorMessage: string | undefined) {
      clearLibraryJobRecord();
      setRecord(undefined);
      setProgress(undefined);
      if (failed && errorMessage) {
        flashError(errorMessage);
      }
      // Both kinds may have changed library shape (new files synced,
      // index rows rebuilt). Invalidate the list query so the grid
      // and any series detail open elsewhere re-fetch.
      queryClient.invalidateQueries({ queryKey: ['series'] });
    }

    void tick();
    return () => {
      cancelled = true;
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [record, queryClient, flashError]);

  const start = useCallback(
    (kind: LibraryJobKind, path: string) => {
      let cancelled = false;
      void (async () => {
        try {
          const handle = await api<{ jobId: string; submittedAt: string }>(path, {
            method: 'POST',
            body: JSON.stringify({}),
          });
          if (cancelled) {
            return;
          }
          const next: LibraryJobRecord = {
            state: 'running',
            kind,
            jobId: handle.jobId,
            startedAt: handle.submittedAt,
          };
          writeLibraryJobRecord(next);
          setRecord(next);
          setProgress(undefined);
        } catch (err) {
          if (cancelled) {
            return;
          }
          const message = err instanceof Error ? err.message : `Failed to start ${kind}`;
          flashError(message);
        }
      })();
      return () => {
        cancelled = true;
      };
    },
    [flashError],
  );

  const startScan = useCallback(() => {
    start('scan', '/api/v1/library/scan');
  }, [start]);
  const startReindex = useCallback(() => {
    start('reindex', '/api/v1/library/reindex');
  }, [start]);

  if (!record) {
    return { phase: 'idle', lastError, startScan, startReindex };
  }
  return {
    phase: 'running',
    kind: record.kind,
    progress: progress
      ? {
          current: progress.current,
          total: progress.total,
          message: progress.message ?? '',
        }
      : undefined,
    jobId: record.jobId,
    lastError,
    startScan,
    startReindex,
  };
}
