import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';

import { KuraApiError, api } from '@/api/client';
import type { JobError, JobStatus, ScanResult, ScanSkip } from '@/api/types.gen';
import {
  type ScanRecord,
  clearScanRecord,
  readScanRecord,
  subscribeScanRecord,
  writeScanRecord,
} from '@/lib/scanState';

const POLL_INTERVAL_MS = 500;

/**
 * `useScanJob` is the per-series scan state machine. It drives the
 * scan-button UI through four phases (idle / running / warning / error)
 * and persists running + terminal state in localStorage so a navigation
 * away or a reload doesn't lose the user's scan.
 *
 * Polling, not SSE: browser EventSource cannot attach the Authorization
 * header in token-auth mode, and the server already polls the registry
 * at 250 ms internally — a 500 ms client poll adds negligible latency
 * and sidesteps the EventSource auth + named-error footguns.
 */
export interface ScanJobState {
  phase: 'idle' | 'running' | 'warning' | 'error';
  /** Latest progress from the server (only meaningful while running). */
  progress?: {
    current: number;
    total: number;
    message: string;
  };
  /** Last-scanned wall-clock label, only used for the idle caption. */
  skipped?: ScanSkip[];
  error?: JobError;
  progressFrozen: number;
  jobId?: string;
  kickoff: () => void;
  dismiss: () => void;
}

export function useScanJob(metadataRef: string): ScanJobState {
  const queryClient = useQueryClient();
  const [record, setRecord] = useState<ScanRecord | undefined>(() => readScanRecord(metadataRef));
  const [progress, setProgress] = useState<JobStatus['progress'] | undefined>(undefined);
  const lastProgressRef = useRef(0);

  // Cross-tab: another tab kicked off / completed a scan for this same
  // series. Mirror its scanState so this tab doesn't render stale.
  useEffect(() => {
    return subscribeScanRecord(metadataRef, (next) => {
      setRecord(next);
      if (!next || next.state !== 'running') {
        setProgress(undefined);
      }
    });
  }, [metadataRef]);

  // Poll job status while running. The poll loop owns its own
  // cancellation flag so a state transition mid-fetch can't race a
  // stale write back into localStorage.
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
          if (status.progress.total > 0) {
            lastProgressRef.current = clamp01(status.progress.current / status.progress.total);
          }
        }
        if (status.state === 'succeeded') {
          handleTerminalSuccess(status);
          return;
        }
        if (status.state === 'failed') {
          handleTerminalError(status);
          return;
        }
      } catch (err) {
        if (cancelled) {
          return;
        }
        if (err instanceof KuraApiError && err.status === 404) {
          // Registry forgot the job (kura restart, TTL eviction). Clear
          // the stale record and drop back to idle.
          clearScanRecord(metadataRef);
          setRecord(undefined);
          setProgress(undefined);
          return;
        }
        // Transient network blip — keep polling. Don't promote to error.
      }
      timer = setTimeout(tick, POLL_INTERVAL_MS);
    }

    function handleTerminalSuccess(status: JobStatus) {
      const result = status.result as ScanResult | undefined;
      const skipped = result?.skipped ?? [];
      if (skipped.length === 0) {
        clearScanRecord(metadataRef);
        setRecord(undefined);
      } else {
        const next: ScanRecord = {
          state: 'warning',
          jobId,
          skipped,
          finishedAt: status.endedAt ?? new Date().toISOString(),
        };
        writeScanRecord(metadataRef, next);
        setRecord(next);
      }
      setProgress(undefined);
      // Either way, the Show + library list need a refetch — lastScanned
      // moved on the server and the episode spine may have changed.
      queryClient.invalidateQueries({ queryKey: ['series', 'show', metadataRef] });
      queryClient.invalidateQueries({ queryKey: ['series'] });
    }

    function handleTerminalError(status: JobStatus) {
      const error: JobError = status.error ?? {
        kind: 'unknown',
        message: 'Scan ended without a result',
      };
      const next: ScanRecord = {
        state: 'error',
        jobId,
        error,
        progressFrozen: lastProgressRef.current,
        finishedAt: status.endedAt ?? new Date().toISOString(),
      };
      writeScanRecord(metadataRef, next);
      setRecord(next);
      setProgress(undefined);
      // Even on error, lastScanned may have updated and the Show fetch
      // catches any partial state. Worth invalidating.
      queryClient.invalidateQueries({ queryKey: ['series', 'show', metadataRef] });
    }

    void tick();
    return () => {
      cancelled = true;
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, [record, metadataRef, queryClient]);

  const kickoff = useCallback(() => {
    let cancelled = false;
    lastProgressRef.current = 0;
    void (async () => {
      try {
        const handle = await api<{ jobId: string; submittedAt: string }>(
          `/api/v1/series/${encodeURIComponent(metadataRef)}/scan`,
          { method: 'POST', body: JSON.stringify({}) },
        );
        if (cancelled) {
          return;
        }
        const next: ScanRecord = {
          state: 'running',
          jobId: handle.jobId,
          startedAt: handle.submittedAt,
        };
        writeScanRecord(metadataRef, next);
        setRecord(next);
        setProgress(undefined);
      } catch (err) {
        if (cancelled) {
          return;
        }
        const message = err instanceof Error ? err.message : 'Failed to start scan';
        const next: ScanRecord = {
          state: 'error',
          jobId: '',
          error: { kind: 'kickoff', message },
          progressFrozen: 0,
          finishedAt: new Date().toISOString(),
        };
        writeScanRecord(metadataRef, next);
        setRecord(next);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [metadataRef]);

  const dismiss = useCallback(() => {
    clearScanRecord(metadataRef);
    setRecord(undefined);
    setProgress(undefined);
  }, [metadataRef]);

  if (!record) {
    return {
      phase: 'idle',
      progressFrozen: 0,
      kickoff,
      dismiss,
    };
  }
  if (record.state === 'running') {
    return {
      phase: 'running',
      progress: progress
        ? {
            current: progress.current,
            total: progress.total,
            message: progress.message ?? '',
          }
        : undefined,
      progressFrozen: lastProgressRef.current,
      jobId: record.jobId,
      kickoff,
      dismiss,
    };
  }
  if (record.state === 'warning') {
    return {
      phase: 'warning',
      skipped: record.skipped,
      progressFrozen: 1,
      jobId: record.jobId,
      kickoff,
      dismiss,
    };
  }
  return {
    phase: 'error',
    error: record.error,
    progressFrozen: record.progressFrozen,
    jobId: record.jobId,
    kickoff,
    dismiss,
  };
}

function clamp01(n: number): number {
  if (Number.isNaN(n)) {
    return 0;
  }
  if (n < 0) {
    return 0;
  }
  if (n > 1) {
    return 1;
  }
  return n;
}
