import { RefreshCw } from 'lucide-react';
import { useEffect, useLayoutEffect, useRef } from 'react';

import { type ScanJobState, useScanJob } from '@/api/scanJob';
import { formatRelativeAgo } from '@/lib/relativeTime';

import styles from './ScanButton.module.css';

interface ScanButtonProps {
  metadataRef: string;
  /** ISO-8601 timestamp from `Show.lastScanned`. Drives the idle caption right slot. */
  lastScanned?: string;
  /** Opens the details modal. The button knows when the link is shown
   *  (warning/error) but not how to render it — the parent owns the modal. */
  onShowDetails: (state: ScanJobState) => void;
}

/**
 * Series-detail "Scan now" button with bottom-edge hairline progress
 * and a caption row below. Visual contract is documented in
 * scratch/Scan Button - Reference.html. State + persistence live in
 * `useScanJob`; this component renders + handles the anti-flicker
 * progress reset.
 */
export function ScanButton({ metadataRef, lastScanned, onShowDetails }: ScanButtonProps) {
  const scan = useScanJob(metadataRef);
  const fillRef = useRef<HTMLSpanElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const previousPhase = useRef(scan.phase);

  // Anti-flicker: when transitioning from idle/warning/error → running,
  // the previous run's --p (likely 1) lingers and the new fill would
  // animate from full to live progress. Force --p=0 with the width
  // transition suppressed for one frame, then let the live updates take
  // over.
  useLayoutEffect(() => {
    const fill = fillRef.current;
    const btn = buttonRef.current;
    if (!fill || !btn) {
      return;
    }
    if (scan.phase === 'running' && previousPhase.current !== 'running') {
      const prev = fill.style.transition;
      fill.style.transition = 'none';
      btn.style.setProperty('--p', '0');
      // Read offsetWidth to flush the suppressed transition before the
      // next paint. `void` keeps the result un-assigned so dead-code
      // elimination can't strip it.
      void fill.offsetWidth;
      fill.style.transition = prev;
    }
    previousPhase.current = scan.phase;
  }, [scan.phase]);

  // Drive --p from progress + frozen markers. The hairline reads --p
  // directly via calc(var(--p) * 100%).
  useEffect(() => {
    const btn = buttonRef.current;
    if (!btn) {
      return;
    }
    const p = computeProgress(scan);
    btn.style.setProperty('--p', String(p));
  }, [scan]);

  const indeterminate = scan.phase === 'running' && (!scan.progress || scan.progress.total <= 0);
  const label = scan.phase === 'running' ? 'Scanning…' : 'Scan now';
  const captionLeft = computeCaptionLeft(scan);
  const captionRight = computeCaptionRight(scan, lastScanned);

  return (
    <div className={styles.scanCol} data-state={scan.phase}>
      <button
        ref={buttonRef}
        type="button"
        className={styles.button}
        data-state={scan.phase}
        data-indeterminate={indeterminate ? 'true' : 'false'}
        disabled={scan.phase === 'running'}
        onClick={() => scan.kickoff()}
      >
        <RefreshCw aria-hidden="true" className={styles.icon} />
        <span>{label}</span>
        <span ref={fillRef} className={styles.hairlineFill} aria-hidden="true" />
        <span className={styles.hairlineTrack} aria-hidden="true" />
      </button>
      <div className={styles.captionRow} aria-live="polite">
        <span className={styles.captionLeft}>{captionLeft}</span>
        <span className={styles.captionRight}>{captionRight}</span>
        <button type="button" className={styles.captionMore} onClick={() => onShowDetails(scan)}>
          more →
        </button>
      </div>
    </div>
  );
}

function computeProgress(scan: ScanJobState): number {
  if (scan.phase === 'running') {
    if (scan.progress && scan.progress.total > 0) {
      return clamp01(scan.progress.current / scan.progress.total);
    }
    return 0;
  }
  if (scan.phase === 'warning') {
    return 1;
  }
  if (scan.phase === 'error') {
    return scan.progressFrozen;
  }
  return 0;
}

function computeCaptionLeft(scan: ScanJobState): string {
  if (scan.phase === 'running') {
    return scan.progress?.message ?? 'Starting scan…';
  }
  if (scan.phase === 'warning') {
    const n = scan.skipped?.length ?? 0;
    return n === 1 ? '1 file was skipped' : `${n} files were skipped`;
  }
  if (scan.phase === 'error') {
    return 'Scan failed';
  }
  return '';
}

function computeCaptionRight(scan: ScanJobState, lastScanned: string | undefined): string {
  if (scan.phase === 'running') {
    const p = scan.progress;
    if (p && p.total > 0) {
      return `${p.current} / ${p.total}`;
    }
    return p && p.current > 0 ? `${p.current} scanned` : '';
  }
  if (scan.phase === 'idle') {
    return lastScanned ? `last scanned ${formatRelativeAgo(lastScanned)}` : 'never scanned';
  }
  return '';
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
