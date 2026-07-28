import { forwardRef, type HTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

interface CardProps extends HTMLAttributes<HTMLDivElement> {
  /**
   * Adds hover lift + amplified shadow + pointer cursor. Use on
   * elements the user clicks (poster cells, list rows, settings
   * affordances). Leave off for purely informational surfaces.
   */
  interactive?: boolean;
}

/**
 * Levitating surface — the workhorse container. Renders the
 * `--shadow-card` two-tier recipe over `--color-surface` with the
 * default 10px radius. Borders are intentionally absent: shadow does
 * the elevation work.
 *
 * Padding is the caller's responsibility; cards compose with whatever
 * spacing the host content needs.
 */
export const Card = forwardRef<HTMLDivElement, CardProps>(
  ({ className, interactive, ...rest }, ref) => (
    <div
      ref={ref}
      className={cn(
        'bg-surface text-ink rounded-md shadow-card',
        interactive &&
          'cursor-pointer transition-all duration-normal ease-out-soft hover:-translate-y-0.5 hover:shadow-card-hover',
        className,
      )}
      {...rest}
    />
  ),
);
Card.displayName = 'Card';
