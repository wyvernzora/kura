import type { EpisodeStatus } from '@/api/types';
import { cn } from '@/lib/cn';
import { EPISODE_STATUS_DOT_BG, EPISODE_STATUS_LABEL } from '@/lib/episodeStatus';

interface StatusDotProps {
  status: EpisodeStatus;
  /**
   * Render the amber pulsing halo that signals a staged change is
   * queued for this episode. Independent of the base dot color so
   * the row keeps showing the on-disk reality (present / missing /
   * pending) underneath the in-motion indicator.
   */
  staged?: boolean;
  className?: string;
}

/**
 * Small status indicator (10 px) used in episode rows. The full label
 * lives on `title=` for hover tooltips and `aria-label` for assistive
 * tech — the dot itself carries no text.
 */
export function StatusDot({ status, staged, className }: StatusDotProps) {
  const baseLabel = EPISODE_STATUS_LABEL[status];
  const label =
    staged && status !== 'staged' && status !== 'staged_replacement'
      ? `${baseLabel} · staged change pending`
      : baseLabel;
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={cn(
        'inline-block h-[10px] w-[10px] shrink-0 rounded-full',
        EPISODE_STATUS_DOT_BG[status],
        staged && 'kura-staged-dot',
        className,
      )}
    />
  );
}
