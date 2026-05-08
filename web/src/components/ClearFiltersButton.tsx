import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';

interface ClearFiltersButtonProps {
  onClick: () => void;
  className?: string;
}

/**
 * Resets every active filter on the library home in one click.
 *
 * Styled as a Material-style text button: transparent at rest with
 * dimmed (muted) foreground; subtle overlay tint on hover. No card
 * chrome (border / shadow) so it reads as a tertiary action next to
 * the levitating filter pills.
 *
 * Mirrors the filter-pill collapse pattern: text + icon at lg+, square
 * icon-only below lg so it stays in step with the dropdown triggers
 * losing their labels at the same breakpoint. Caller gates render on
 * `active.size + activeSources.size + activeResolutions.size > 0` so
 * the affordance only shows when there's something to clear.
 */
export function ClearFiltersButton({ onClick, className }: ClearFiltersButtonProps) {
  // Shared chrome: transparent background, muted foreground, subtle
  // overlay tint on hover — Material Design text-button recipe.
  const base = cn(
    'inline-flex h-9 items-center justify-center rounded-md text-muted',
    'transition-[background-color,color] duration-[160ms] ease-out',
    'hover:bg-overlay-soft hover:text-ink',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
  );
  return (
    <>
      {/* < lg — square icon-only button. */}
      <button
        type="button"
        aria-label="Clear filters"
        onClick={onClick}
        className={cn(base, 'w-9 lg:hidden', className)}
      >
        <MaterialIcon name="filter_alt_off" size={18} />
      </button>
      {/* ≥ lg — text + icon. */}
      <button
        type="button"
        aria-label="Clear filters"
        onClick={onClick}
        className={cn(base, 'hidden gap-2 px-3 text-sm font-medium lg:inline-flex', className)}
      >
        <MaterialIcon name="filter_alt_off" size={16} />
        <span>Clear</span>
      </button>
    </>
  );
}
