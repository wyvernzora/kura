import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { type Priority, priorityFromTags } from '@/lib/seriesSettings';

const PRIORITY_BADGES: Record<
  Exclude<Priority, 'normal'>,
  { icon: string; label: string; className: string }
> = {
  high: {
    icon: 'keyboard_double_arrow_up',
    label: 'High',
    className: 'bg-priority-high text-priority-high-fg',
  },
  low: {
    icon: 'keyboard_double_arrow_down',
    label: 'Low',
    className: 'bg-priority-low text-priority-low-fg',
  },
  disabled: {
    icon: 'block',
    label: 'Disabled',
    className: 'bg-priority-disabled text-priority-disabled-fg',
  },
};

interface PosterPriorityBadgeProps {
  tags?: readonly string[];
  /**
   * `corner` (default) anchors a square chip to the poster's bottom-left,
   * mirroring EpisodeCountBadge. `round` renders an unpositioned circular
   * chip sized to sit inline beside SeriesStatusCornerPill (h-[22px]).
   */
  variant?: 'corner' | 'round';
}

const VARIANT_CLASSES = {
  corner: 'absolute left-1.5 bottom-1.5 z-[3] rounded-[4px] px-1 py-0.5',
  round: 'h-[22px] w-[22px] justify-center rounded-full',
};

const VARIANT_SHADOWS = {
  corner: '0 1px 2px rgba(0,0,0,0.18), 0 0 0 1.5px rgba(255,255,255,0.45)',
  round: '0 1px 2px rgba(0,0,0,0.18), 0 0 0 1.5px rgba(255,255,255,0.6)',
};

export function PosterPriorityBadge({ tags, variant = 'corner' }: PosterPriorityBadgeProps) {
  const priority = priorityFromTags(tags ?? []);
  if (priority === 'normal') {
    return null;
  }

  const badge = PRIORITY_BADGES[priority];
  return (
    <span
      aria-label={`${badge.label} priority`}
      className={cn('inline-flex items-center', VARIANT_CLASSES[variant], badge.className)}
      style={{ boxShadow: VARIANT_SHADOWS[variant] }}
    >
      <MaterialIcon name={badge.icon} size={13} />
    </span>
  );
}
