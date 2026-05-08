import { type HTMLAttributes, forwardRef } from 'react';

import { cn } from '@/lib/cn';

interface SurfaceProps extends HTMLAttributes<HTMLDivElement> {
  /**
   * Adds a subtle border (`--color-line-soft`). Useful when a Surface
   * sits directly on the page background and needs a faint outline to
   * read as a discrete region without claiming elevation.
   */
  bordered?: boolean;
}

/**
 * Flat surface with no elevation. Used for grouping containers that
 * should read as a region but not claim attention by floating. Pair
 * with `bordered` when sitting on the page background; leave the
 * border off when nested inside a Card.
 */
export const Surface = forwardRef<HTMLDivElement, SurfaceProps>(
  ({ className, bordered, ...rest }, ref) => (
    <div
      ref={ref}
      className={cn(
        'bg-surface text-ink rounded-md',
        bordered && 'border border-line-soft',
        className,
      )}
      {...rest}
    />
  ),
);
Surface.displayName = 'Surface';
