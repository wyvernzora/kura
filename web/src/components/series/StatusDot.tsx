import type { EpisodeStatus } from '@/api/types';
import { cn } from '@/lib/cn';
import { EPISODE_STATUS_DOT_BG, EPISODE_STATUS_LABEL } from '@/lib/episodeStatus';

interface StatusDotProps {
  status: EpisodeStatus;
  className?: string;
}

/**
 * Small status indicator (10 px) used in episode rows. The full label
 * lives on `title=` for hover tooltips and `aria-label` for assistive
 * tech — the dot itself carries no text.
 */
export function StatusDot({ status, className }: StatusDotProps) {
  const label = EPISODE_STATUS_LABEL[status];
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={cn(
        'inline-block h-[10px] w-[10px] shrink-0 rounded-full',
        EPISODE_STATUS_DOT_BG[status],
        className,
      )}
    />
  );
}
