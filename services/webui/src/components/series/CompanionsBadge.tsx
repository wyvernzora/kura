import { cn } from '@/lib/cn';

interface CompanionsBadgeProps {
  count: number;
  className?: string;
}

/**
 * Tiny `+N` pill rendered next to an episode title when companion
 * files (subs / fonts / chapters) sit alongside the main video.
 * Renders nothing for zero so callers can spread it inline without a
 * conditional.
 */
export function CompanionsBadge({ count, className }: CompanionsBadgeProps) {
  if (!count) {
    return null;
  }
  const noun = count === 1 ? 'companion file' : 'companion files';
  return (
    <span
      title={`${count} ${noun}`}
      className={cn(
        'ml-1.5 inline-flex h-4 items-center rounded-full bg-line-soft px-1.5',
        'font-mono text-[9px] font-semibold tracking-[0.3px] text-muted',
        className,
      )}
    >
      +{count}
    </span>
  );
}
