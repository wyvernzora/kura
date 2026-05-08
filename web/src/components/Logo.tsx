import { cn } from '@/lib/cn';

interface LogoProps {
  className?: string;
}

/**
 * Kura wordmark — 36×36 ink-filled square with a paper "K" in Inter.
 * Custom shadow recipe (tighter than shadow-card) so the mark reads
 * as a solid stamp rather than a floating chip. Treatment ports
 * verbatim from scratch/webui-prototype/.
 *
 * Brand polish is a P1+ task — the mark itself is the placeholder.
 */
export function Logo({ className }: LogoProps) {
  return (
    <div
      aria-label="kura"
      className={cn(
        'inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-[8px]',
        'bg-ink text-paper',
        'shadow-[0_1px_2px_rgba(31,29,26,0.18),0_4px_12px_rgba(31,29,26,0.12)]',
        'font-sans font-bold text-[18px] leading-none tracking-[-0.5px]',
        className,
      )}
    >
      K
    </div>
  );
}
