import { cn } from '@/lib/cn';
import {
  primaryStatus,
  STATUS_LABELS,
  type Status,
  type StatusValue,
  secondaryStatus,
} from '@/lib/status';

interface StatusChipProps {
  status: StatusValue;
  className?: string;
  size?: 'sm' | 'md';
}

/**
 * Background + foreground recipes per status. Tailwind v4 generates
 * `bg-status-*` and `text-status-*-fg` from the @theme tokens; the
 * map keeps the dynamic lookup statically analyzable (no string
 * interpolation that the JIT can't see).
 */
const CHIP_STYLES: Record<Status, string> = {
  complete: 'bg-status-complete text-status-complete-fg',
  incomplete: 'bg-status-incomplete text-status-incomplete-fg',
  airing: 'bg-status-airing text-status-airing-fg',
  untracked: 'bg-status-untracked text-status-untracked-fg',
  error: 'bg-status-error text-status-error-fg',
};

const ACCENT_BG: Record<Status, string> = {
  complete: 'bg-status-complete',
  incomplete: 'bg-status-incomplete',
  airing: 'bg-status-airing',
  untracked: 'bg-status-untracked',
  error: 'bg-status-error',
};

/**
 * Compact pill conveying a series' rolled-up status. Mono font + tight
 * tracking so it reads as a label, not body text. Compound state
 * (e.g. `["airing", "incomplete"]`) renders the precedence-winning
 * status with a small dot in the secondary color so the dual-state
 * doesn't disappear.
 */
export function StatusChip({ status, className, size = 'sm' }: StatusChipProps) {
  const primary = primaryStatus(status);
  const secondary = secondaryStatus(status);
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-sm font-mono font-medium uppercase tracking-wide',
        CHIP_STYLES[primary],
        size === 'sm' && 'h-5 px-1.5 text-[10px]',
        size === 'md' && 'h-6 px-2 text-xs',
        className,
      )}
    >
      {secondary && (
        <span
          aria-hidden="true"
          className={cn('h-1.5 w-1.5 rounded-full ring-1 ring-white/40', ACCENT_BG[secondary])}
        />
      )}
      {STATUS_LABELS[primary]}
    </span>
  );
}
