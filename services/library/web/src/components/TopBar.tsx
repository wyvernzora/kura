import { Link, useRouterState } from '@tanstack/react-router';
import { ArrowLeft } from 'lucide-react';

import { GearMenu } from '@/components/GearMenu';
import { Logo } from '@/components/Logo';
import { SearchField } from '@/components/SearchField';
import { cn } from '@/lib/cn';
import { useScrolled } from '@/lib/useScrolled';
import { useSearch } from '@/state/search';

interface TopBarProps {
  className?: string;
  /**
   * Override the scroll-derived "scrolled" state. Used by Storybook
   * stories to render the sticky-on-scroll appearance without an
   * actual scrollable parent. Production consumers leave this unset.
   */
  forceScrolled?: boolean;
}

/**
 * Sticky top chrome. Logo (left) | search field (center) | theme +
 * gear (right). Three-column layout — search lives in a `flex: 1`
 * column so it stays visually centered regardless of the side
 * clusters' widths.
 *
 * On detail routes (`/series/$ref`) the leading slot swaps the kura
 * logo for a "back to library" button; everything else stays put so
 * the chrome reads as the same surface across navigation.
 *
 * At scrollY === 0 the bar is invisible chrome (paper bg, no
 * shadow, no border). On scroll: paper-tinted translucent bg,
 * backdrop blur + saturation, soft drop shadow underneath. Drives
 * the "page peeks under the bar" effect.
 */
export function TopBar({ className, forceScrolled }: TopBarProps) {
  const detected = useScrolled();
  const scrolled = forceScrolled ?? detected;

  const query = useSearch((s) => s.query);
  const setQuery = useSearch((s) => s.setQuery);
  const clear = useSearch((s) => s.clear);

  // Drive the leading slot off the route so callers don't have to
  // pass a mode prop down through AppShell. `useRouterState` returns
  // a frozen value snapshot on each navigation; cheap to read.
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const onDetailRoute = pathname.startsWith('/series/');

  return (
    <header
      className={cn(
        'sticky top-0 z-50',
        'transition-[box-shadow,background-color] duration-normal ease-out-soft',
        scrolled
          ? cn(
              // backdrop-blur radius reduced from md (12 px) to sm
              // (4 px) — the 12 px filter is GPU-expensive on weak
              // iGPUs and runs every scroll frame. The smaller radius
              // gives ~3× the throughput with similar visual.
              'bg-topbar-scrolled backdrop-blur-sm backdrop-saturate-[1.8]',
              'shadow-[0_1px_0_rgba(31,29,26,0.05),0_8px_16px_-10px_rgba(31,29,26,0.12)]',
            )
          : 'bg-paper',
        className,
      )}
    >
      {/*
        Inner content caps at 1920 px and centers; the header
        background still spans the viewport so the scroll-shadow
        runs edge-to-edge. Side padding equals the logo's vertical
        inset from the top edge — (72 - 36) / 2 = 18 px — so the K
        mark sits equidistant from the top and the left when the
        bar isn't constrained to a column.
      */}
      <div className="mx-auto flex h-[72px] max-w-[1920px] items-center gap-3 px-[18px]">
        {onDetailRoute ? <BackToLibrary /> : <Logo />}
        <div className="flex min-w-0 flex-1 justify-center">
          <SearchField
            className="w-full max-w-[560px]"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onClear={clear}
            placeholder="Search library — title, source, year…"
          />
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <GearMenu />
        </div>
      </div>
    </header>
  );
}

/**
 * Pill-shaped link rendered in the leading slot when the user is on
 * a detail route. Sized to the same 36 px IconBtn footprint so the
 * top-bar baseline doesn't shift across navigation.
 */
function BackToLibrary() {
  return (
    <Link
      to="/"
      className={cn(
        'inline-flex h-9 items-center gap-2 rounded-md border border-line-soft bg-surface px-3 text-sm font-medium text-ink shadow-card',
        'transition-[transform,box-shadow,background-color] duration-[160ms] ease-out',
        'hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
      )}
    >
      <ArrowLeft aria-hidden="true" className="h-4 w-4" />
      <span className="hidden sm:inline">Library</span>
    </Link>
  );
}
