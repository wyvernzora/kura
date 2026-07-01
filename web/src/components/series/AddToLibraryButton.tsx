import { useNavigate } from '@tanstack/react-router';
import { Check, Plus } from 'lucide-react';
import { useEffect, useState } from 'react';

import { KuraApiError } from '@/api/client';
import { useAddSeries } from '@/api/hooks';
import { cn } from '@/lib/cn';

/** How long the armed ("Confirm add") state lingers before reverting. */
const DISARM_MS = 3000;

interface AddToLibraryButtonProps {
  /** Metadata ref (provider:id) of the previewed series. */
  metadataRef: string;
}

/**
 * Two-click add for the series preview page. First click arms the
 * button ("Confirm add"); a second click within DISARM_MS commits the
 * add. On success the library list + this series' show query are
 * invalidated (via useAddSeries), so the detail page refetches and
 * flips from preview to the real in-library view — no navigation here.
 */
export function AddToLibraryButton({ metadataRef }: AddToLibraryButtonProps) {
  const [armed, setArmed] = useState(false);
  const addSeries = useAddSeries();
  const navigate = useNavigate();

  useEffect(() => {
    if (!armed) {
      return;
    }
    const t = setTimeout(() => setArmed(false), DISARM_MS);
    return () => clearTimeout(t);
  }, [armed]);

  // Hold the busy label from add-start until the page flips to the
  // in-library view (drop ?preview → useShow refetches local data).
  const busy = addSeries.isPending || addSeries.isSuccess;

  const onClick = () => {
    if (busy) {
      return;
    }
    if (!armed) {
      setArmed(true);
      return;
    }
    addSeries.mutate(
      { ref: metadataRef },
      {
        onSuccess: () => {
          // Drop the preview query param → the page re-renders against
          // local state (the series is now in the index). replace so
          // Back doesn't return to the preview.
          void navigate({
            to: '/series/$ref',
            params: { ref: metadataRef },
            search: {},
            replace: true,
          });
        },
      },
    );
  };

  const label = busy ? 'Adding…' : armed ? 'Confirm add' : 'Add to library';
  const Icon = armed || busy ? Check : Plus;

  return (
    <div className="flex flex-col gap-1.5">
      <button
        type="button"
        onClick={onClick}
        disabled={busy}
        className={cn(
          'kura-focusable inline-flex h-[38px] w-full items-center justify-center gap-2 rounded-md',
          'text-[13px] font-medium shadow-card transition-[background-color,box-shadow,transform] duration-150',
          armed
            ? 'bg-status-complete text-status-complete-fg hover:brightness-105'
            : 'bg-surface text-ink hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
          busy && 'opacity-85',
        )}
      >
        <Icon aria-hidden="true" className="h-4 w-4" />
        <span>{label}</span>
      </button>
      {armed && !busy && (
        <span className="px-1 font-mono text-[10px] tracking-[0.4px] text-muted">
          click again to confirm
        </span>
      )}
      {addSeries.isError && (
        <span className="px-1 text-xs text-status-error">
          {addSeries.error instanceof KuraApiError
            ? (addSeries.error.body?.message ?? 'Failed to add series.')
            : 'Failed to add series.'}
        </span>
      )}
    </div>
  );
}
