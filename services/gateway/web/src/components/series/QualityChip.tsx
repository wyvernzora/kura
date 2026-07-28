import { cn } from '@/lib/cn';
import {
  isSubOptimalResolution,
  isSubOptimalSource,
  type QualityBucket,
  resolutionBucket,
  sourceBucket,
} from '@/lib/episodeStatus';

type ChipSize = 'default' | 'compact';

interface QualityChipProps {
  /** Display label, e.g. "BluRay", "1080p". */
  children: string;
  /** Color bucket — drives both stroke + fill. */
  bucket: QualityBucket;
  /** When true, render filled (background = bucket color). */
  filled: boolean;
  /**
   * `default` (22 × 64) is the desktop episode-row chip — wide enough
   * to give every label the same horizontal footprint. `compact`
   * (18 × content) trades the fixed grid for content-width so two
   * chips fit alongside the title in the mobile two-row stack.
   */
  size?: ChipSize;
  className?: string;
}

/**
 * Outline (or filled) per-bucket maps. Tailwind v4 generates the
 * `bg-status-*`, `text-status-*`, `border-status-*` classes from the
 * `@theme` tokens; the maps keep the dynamic lookup statically
 * analyzable so the JIT can't drop them.
 */
const OUTLINED: Record<QualityBucket, string> = {
  airing: 'border-status-airing text-status-airing',
  complete: 'border-status-complete text-status-complete',
  incomplete: 'border-status-incomplete text-status-incomplete',
  error: 'border-status-error text-status-error',
  untracked: 'border-status-untracked text-status-untracked',
};

const FILLED: Record<QualityBucket, string> = {
  airing: 'border-status-airing bg-status-airing text-status-airing-fg',
  complete: 'border-status-complete bg-status-complete text-status-complete-fg',
  incomplete: 'border-status-incomplete bg-status-incomplete text-status-incomplete-fg',
  error: 'border-status-error bg-status-error text-status-error-fg',
  untracked: 'border-status-untracked bg-status-untracked text-status-untracked-fg',
};

const SIZE_STYLES: Record<ChipSize, string> = {
  default: 'h-[22px] w-[64px] text-[9px] tracking-[0.4px]',
  compact: 'h-[18px] px-1.5 text-[8px] tracking-[0.3px]',
};

/**
 * Quality chip for source / resolution. Default sizing is fixed-width
 * (64 × 22) so columns align in the desktop episode table; compact
 * shrinks to content (h-18, px-1.5) so two chips fit alongside the
 * title in the mobile two-row stack. Premium tiers render outlined;
 * sub-optimal tiers render filled to draw the eye.
 */
function QualityChip({ children, bucket, filled, size = 'default', className }: QualityChipProps) {
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-md border font-mono font-semibold uppercase',
        SIZE_STYLES[size],
        filled ? FILLED[bucket] : OUTLINED[bucket],
        className,
      )}
    >
      {children}
    </span>
  );
}

interface SourceChipProps {
  source: string;
  size?: ChipSize;
  className?: string;
}

export function SourceChip({ source, size, className }: SourceChipProps) {
  return (
    <QualityChip
      bucket={sourceBucket(source)}
      filled={isSubOptimalSource(source)}
      size={size}
      className={className}
    >
      {source}
    </QualityChip>
  );
}

interface ResolutionChipProps {
  resolution: string;
  size?: ChipSize;
  className?: string;
}

export function ResolutionChip({ resolution, size, className }: ResolutionChipProps) {
  return (
    <QualityChip
      bucket={resolutionBucket(resolution)}
      filled={isSubOptimalResolution(resolution)}
      size={size}
      className={className}
    >
      {resolution}
    </QualityChip>
  );
}

/**
 * Empty-slot spacer that takes the same dimensions as a chip — used
 * by missing / pending rows so the columns line up across the table.
 */
export function ChipSlot({ className }: { className?: string }) {
  return <span className={cn('inline-block h-[22px] w-[64px] shrink-0', className)} />;
}
