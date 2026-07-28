import { cn } from '@/lib/cn';
import { STATUS_LABELS, type Status } from '@/lib/status';

interface SeriesStatusCornerPillProps {
  status: Status;
  className?: string;
}

const STYLE: Record<Status, string> = {
  airing: 'bg-status-airing text-status-airing-fg',
  complete: 'bg-status-complete text-status-complete-fg',
  incomplete: 'bg-status-incomplete text-status-incomplete-fg',
  untracked: 'bg-status-untracked text-status-untracked-fg',
  error: 'bg-status-error text-status-error-fg',
};

/**
 * Status pill anchored to the top-right of the series-detail poster
 * art. Mirrors the prototype's halo treatment (`box-shadow` with both
 * a dark drop and a thin white outer ring) so the chip pops against
 * any cover image regardless of theme.
 */
export function SeriesStatusCornerPill({ status, className }: SeriesStatusCornerPillProps) {
  return (
    <span
      className={cn(
        'absolute top-[10px] right-[10px] z-[3] inline-flex h-[22px] items-center rounded-full px-[10px]',
        'font-mono text-[9px] font-bold tracking-[0.6px] uppercase',
        STYLE[status],
        className,
      )}
      style={{
        boxShadow: '0 1px 2px rgba(0,0,0,0.18), 0 0 0 1.5px rgba(255,255,255,0.6)',
      }}
    >
      {STATUS_LABELS[status]}
    </span>
  );
}
