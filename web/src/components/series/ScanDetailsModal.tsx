import * as DialogPrimitive from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { useMemo } from 'react';

import type { JobError, ScanSkip } from '@/api/types.gen';
import { cn } from '@/lib/cn';

interface ScanDetailsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  view: ScanDetailsView;
}

export type ScanDetailsView =
  | { kind: 'warning'; skipped: ScanSkip[] }
  | { kind: 'error'; error: JobError; progressFrozen: number }
  | undefined;

const SKIP_CODE_LABELS: Record<string, string> = {
  special_number_not_inferred: 'Special number not inferred',
  episode_number_not_inferred: 'Episode number not inferred',
  season_mismatch: 'Filename season ≠ directory',
  ignored_directory: 'Directory ignored',
  duplicate_slot: 'Duplicate episode slot',
  metadata_slot_missing: 'No matching metadata slot',
};

export function ScanDetailsModal({ open, onOpenChange, view }: ScanDetailsModalProps) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-[70] bg-overlay backdrop-blur-[2px] data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <DialogPrimitive.Content
          className={cn(
            'fixed top-1/2 left-1/2 z-[71] w-[min(720px,calc(100vw-32px))] max-h-[80vh]',
            '-translate-x-1/2 -translate-y-1/2 overflow-hidden',
            'rounded-lg bg-surface text-ink shadow-pop',
            'flex flex-col',
          )}
        >
          <header className="flex items-start justify-between gap-4 border-b border-line-soft px-6 py-4">
            <DialogPrimitive.Title className="text-base font-semibold tracking-tight">
              {view?.kind === 'warning' ? 'Skipped files' : 'Scan failed'}
            </DialogPrimitive.Title>
            <DialogPrimitive.Close
              className="cursor-pointer text-muted hover:text-ink"
              aria-label="Close"
            >
              <X className="h-4 w-4" aria-hidden="true" />
            </DialogPrimitive.Close>
          </header>
          <div className="flex-1 overflow-y-auto px-6 py-5 text-sm leading-relaxed">
            {view?.kind === 'warning' && <SkippedView skipped={view.skipped} />}
            {view?.kind === 'error' && (
              <ErrorView error={view.error} progressFrozen={view.progressFrozen} />
            )}
            {!view && <p className="text-muted">No details available.</p>}
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

function SkippedView({ skipped }: { skipped: ScanSkip[] }) {
  const grouped = useMemo(() => groupByCode(skipped), [skipped]);
  return (
    <div className="flex flex-col gap-5">
      <p className="text-muted">
        {skipped.length === 1
          ? '1 file was skipped during this scan. Resolve the cause and rescan to pick it up.'
          : `${skipped.length} files were skipped during this scan. Resolve the causes and rescan to pick them up.`}
      </p>
      {grouped.map(([code, items]) => (
        <section key={code}>
          <h3 className="mb-2 text-[11px] font-semibold tracking-wide text-muted uppercase">
            {SKIP_CODE_LABELS[code] ?? code} · {items.length}
          </h3>
          <ul className="flex flex-col gap-1.5 font-mono text-[12px] text-ink-2">
            {items.map((item) => (
              <li key={item.path} className="break-all">
                <span className="text-ink">{item.path}</span>
                {item.reason && <span className="text-muted"> — {item.reason}</span>}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function ErrorView({ error, progressFrozen }: { error: JobError; progressFrozen: number }) {
  const pct = Math.round(progressFrozen * 100);
  return (
    <div className="flex flex-col gap-3">
      <p className="font-mono text-[11px] tracking-wide text-muted uppercase">{error.kind}</p>
      <p className="text-ink">{error.message}</p>
      {progressFrozen > 0 && <p className="text-muted">Scanned {pct}% before failing.</p>}
      {error.data && Object.keys(error.data).length > 0 && (
        <pre className="rounded-sm bg-overlay-soft p-3 font-mono text-[11px] text-ink-2 whitespace-pre-wrap">
          {JSON.stringify(error.data, null, 2)}
        </pre>
      )}
    </div>
  );
}

function groupByCode(skipped: ScanSkip[]): Array<[string, ScanSkip[]]> {
  const map = new Map<string, ScanSkip[]>();
  for (const item of skipped) {
    const list = map.get(item.code);
    if (list) {
      list.push(item);
    } else {
      map.set(item.code, [item]);
    }
  }
  return Array.from(map.entries()).sort((a, b) => a[0].localeCompare(b[0]));
}
