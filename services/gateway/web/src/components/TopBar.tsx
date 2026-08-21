import { Link, useNavigate, useRouterState } from '@tanstack/react-router';
import { ArrowLeft } from 'lucide-react';
import { useEffect, useRef } from 'react';

import { useResolveSearch } from '@/api/hooks';
import { SearchField } from '@/components/SearchField';
import { SearchScopeControl } from '@/components/SearchScope';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { useScrolled } from '@/lib/useScrolled';
import { MIN_TVDB_QUERY_LENGTH, useSearch } from '@/state/search';

interface TopBarProps {
  className?: string;
  /** Opens the mobile navigation drawer. Owned by AppShell. */
  onMenu: () => void;
  /**
   * Override the scroll-derived "scrolled" state. Used by Storybook
   * stories to render the sticky-on-scroll appearance without an
   * actual scrollable parent. Production consumers leave this unset.
   */
  forceScrolled?: boolean;
}

/**
 * Sticky top chrome. Leading slot | search field (center) | empty
 * trailing slot. 3-column grid (`1fr / minmax(0,560px) / 1fr`) on
 * desktop so the search field is centered against the viewport
 * instead of the leftover space — the back-to-library pill is wider
 * than nothing at all, and a flex-based center would shift the field
 * left when those side widths diverge. Below `md` the trailing slot
 * is dropped entirely and the leading slot shrinks to its content:
 * the hamburger has to sit flush left, and a 3-column grid on a
 * 360 px viewport leaves the field too narrow to type in.
 *
 * The leading slot carries the hamburger (mobile only — desktop nav
 * lives in the sidebar) plus, on detail routes (`/series/$ref`), the
 * "back to library" pill. There is no logo here: the kura mark is the
 * sidebar's, and the drawer header's.
 *
 * At scrollY === 0 the bar is invisible chrome (paper bg, no
 * shadow, no border). On scroll: paper-tinted translucent bg,
 * backdrop blur + saturation, soft drop shadow underneath.
 *
 * Search initiated from a non-home route navigates to `/` so the
 * library grid renders the matches; the originating path is captured
 * in the search store so `clear()` can return the user there.
 */
export function TopBar({ className, onMenu, forceScrolled }: TopBarProps) {
  const detected = useScrolled();
  const scrolled = forceScrolled ?? detected;
  const navigate = useNavigate();

  const query = useSearch((s) => s.query);
  const setQuery = useSearch((s) => s.setQuery);
  const setOrigin = useSearch((s) => s.setOrigin);
  const clear = useSearch((s) => s.clear);
  const scope = useSearch((s) => s.scope);
  const setScope = useSearch((s) => s.setScope);

  const trimmed = query.trim();
  const searching = trimmed.length > 0;
  const tvdbEligible = trimmed.length >= MIN_TVDB_QUERY_LENGTH;
  // Prefetch: the resolve fires as soon as the query is long enough
  // (its own debounce applies), regardless of scope — results are
  // warm by the time the user opens the TVDB tab. Shares the
  // react-query cache with the home route's reader, so this costs no
  // extra request. Below the floor the hook receives '' and stays
  // disabled.
  const resolve = useResolveSearch(tvdbEligible ? trimmed : '');

  // Drive the leading slot off the route so callers don't have to
  // pass a mode prop down through AppShell. `useRouterState` returns
  // a frozen value snapshot on each navigation; cheap to read.
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const onDetailRoute = pathname.startsWith('/series/');

  // ⌘K (or Ctrl-K) focuses the search input from anywhere on the
  // page. The hint pill in SearchField promises this; the listener
  // below honors it. preventDefault so the browser doesn't open its
  // own URL-bar shortcut.
  const searchRef = useRef<HTMLInputElement | null>(null);
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if (e.key !== 'k' && e.key !== 'K') {
        return;
      }
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) {
        return;
      }
      const input = searchRef.current;
      if (!input) {
        return;
      }
      e.preventDefault();
      input.focus();
      input.select();
    }
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  function handleSearchChange(next: string) {
    // Capture the origin only on the empty → non-empty transition
    // while the user is somewhere other than home. Subsequent
    // keystrokes leave `origin` alone.
    if (next.length > 0 && query.length === 0 && pathname !== '/') {
      setOrigin(pathname);
      void navigate({ to: '/' });
      setQuery(next);
      return;
    }
    // Backspacing all the way to empty rewinds the same way the
    // explicit × button does — `clear()` resets origin so this stays
    // a one-shot session.
    if (next.length === 0 && query.length > 0) {
      rewindClear();
      return;
    }
    setQuery(next);
  }

  function rewindClear() {
    const origin = useSearch.getState().origin;
    clear();
    // Only rewind if the user is still on the search-results view
    // (`/`). If they've already navigated forward into a result,
    // emptying the input should just clear the query in place.
    if (origin && pathname === '/') {
      void navigate({ to: origin });
    }
  }

  return (
    <header className={cn(barChrome(scrolled), className)}>
      {/*
        Inner content caps at 1920 px and centers; the header
        background still spans the viewport so the scroll-shadow
        runs edge-to-edge. Side padding equals the logo's vertical
        inset from the top edge — (72 - 36) / 2 = 18 px — so the K
        mark sits equidistant from the top and the left when the
        bar isn't constrained to a column.
      */}
      <div className="mx-auto grid h-[72px] max-w-[1920px] grid-cols-[auto_minmax(0,1fr)] items-center gap-3 px-[18px] md:grid-cols-[1fr_minmax(0,560px)_1fr]">
        <div className="flex items-center gap-2 justify-self-start">
          <HamburgerButton onClick={onMenu} className="md:hidden" />
          {onDetailRoute && <BackToLibrary />}
        </div>
        {/* SearchField shrinks to make room for the scope control,
            which only exists during an active search session. */}
        <div className="flex min-w-0 items-center gap-2">
          <SearchField
            ref={searchRef}
            className="min-w-0 flex-1"
            value={query}
            onChange={(e) => handleSearchChange(e.target.value)}
            onClear={rewindClear}
            placeholder="Search library — title or alias…"
          />
          {searching && (
            <SearchScopeControl
              scope={scope}
              onScope={setScope}
              tvdbFetching={resolve.isFetching}
              tvdbDisabled={!tvdbEligible}
            />
          )}
        </div>
        {/* Trailing slot: empty, but it holds the third grid column
            open so the search field stays centered on the viewport.
            Dropped below md, where the grid is 2-column. */}
        <div className="hidden md:block" />
      </div>
    </header>
  );
}

/**
 * Sticky-bar shell shared by the top bar and the Settings mobile bar.
 * At rest the bar is invisible chrome (paper bg); once the page
 * scrolls it floats: paper-tinted translucent bg, backdrop blur +
 * saturation, soft drop shadow underneath.
 */
function barChrome(scrolled: boolean): string {
  return cn(
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
  );
}

/**
 * Slim Settings chrome for mobile. Settings has no search field, so
 * on desktop AppShell renders no bar at all; below `md` the hamburger
 * still has to live somewhere or the drawer becomes unreachable. The
 * bar's "Settings" title doubles as the page heading on mobile —
 * SettingsPage hides its h1 below `md` so the word appears once.
 */
export function SettingsMobileBar({ onMenu }: { onMenu: () => void }) {
  const scrolled = useScrolled();
  return (
    <header className={cn(barChrome(scrolled), 'md:hidden')}>
      <div className="flex h-[72px] items-center gap-3 px-[18px]">
        <HamburgerButton onClick={onMenu} />
        <span className="font-semibold text-[15px] text-ink tracking-[-0.2px]">Settings</span>
      </div>
    </header>
  );
}

/**
 * Drawer trigger. Deliberately matches SearchField's shell exactly —
 * 38 px, radius 8, surface fill, card shadow, no border — so the two
 * read as one control strip rather than a button beside a field.
 */
function HamburgerButton({ onClick, className }: { onClick: () => void; className?: string }) {
  return (
    <button
      type="button"
      aria-label="Open menu"
      onClick={onClick}
      className={cn(
        'inline-flex h-[38px] w-[38px] shrink-0 cursor-pointer items-center justify-center',
        'rounded-[8px] bg-surface text-ink shadow-card',
        className,
      )}
    >
      <MaterialIcon name="menu" size={18} />
    </button>
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
