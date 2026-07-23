import { Link, useRouterState } from '@tanstack/react-router';

import { cn } from '@/lib/cn';
import { useLibraryFilters } from '@/state/library';
import { useSearch } from '@/state/search';

interface LogoProps {
  className?: string;
  /**
   * When true (the default), the logo renders as a Link to `/` and
   * clears filters + search + scroll on click — bringing the home
   * page back to its first-visit state. Set false (used by GearMenu's
   * about panel) to render the mark as a static visual element.
   */
  interactive?: boolean;
}

/**
 * Kura wordmark — 36×36 ink-filled square with a paper "K" in Inter.
 * Custom shadow recipe (tighter than shadow-card) so the mark reads
 * as a solid stamp rather than a floating chip. Treatment ports
 * verbatim from scratch/webui-prototype/.
 *
 * Click / tap behavior (when interactive): navigate to `/`, clear
 * library filters + sort, clear the search query, scroll the window
 * to the top. The combined effect is "reset the home page to its
 * first-visit state." A single tap is the user's escape hatch out of
 * any filtered / scrolled / searched view.
 *
 * Brand polish is a P1+ task — the mark itself is the placeholder.
 */
export function Logo({ className, interactive = true }: LogoProps) {
  const clearFilters = useLibraryFilters((s) => s.clear);
  const clearSearch = useSearch((s) => s.clear);
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const square = (
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

  if (!interactive) {
    return square;
  }

  function handleClick(e: React.MouseEvent<HTMLAnchorElement>) {
    // Reset the home page to first-visit state. Even when the user
    // is already on `/` (and the <Link> navigation is a no-op), this
    // still clears state and scrolls — that's the whole point of
    // tapping the logo.
    clearFilters();
    clearSearch();
    if (pathname === '/') {
      // No route change → no router scroll-restoration kick →
      // scroll manually. `auto` here matches the browser's
      // default snap behavior rather than animating a long
      // smooth-scroll for users far down the grid.
      e.preventDefault();
      window.scrollTo({ top: 0, behavior: 'auto' });
    }
    // When pathname !== '/', the Link's own navigation handles the
    // page change; the router lands at the top by default on a
    // fresh push. No manual scroll call needed.
  }

  return (
    <Link
      to="/"
      onClick={handleClick}
      aria-label="kura — reset library view"
      className="inline-flex"
    >
      {square}
    </Link>
  );
}
