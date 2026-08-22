import { useRouterState } from '@tanstack/react-router';
import { type ReactNode, useState } from 'react';

import { MobileDrawer } from '@/components/MobileDrawer';
import { Sidebar } from '@/components/Sidebar';
import { PageBarIconButton, PageMobileBar, TopBar } from '@/components/TopBar';
import { MaterialIcon } from '@/components/ui/material-icon';
import { RELEASES_REFRESH_EVENT } from '@/lib/releases';
import { useSuppressHoverOnScroll } from '@/lib/useSuppressHoverOnScroll';
import { useReleasesView } from '@/state/releasesView';

interface AppShellProps {
  children: ReactNode;
}

/**
 * Layout chrome for every route: a sticky navigation sidebar (md+)
 * beside a page column carrying the top bar and the content. The route
 * tree owns the actual page content via `<Outlet />` (passed in as
 * `children` by the root route) so AppShell stays presentational and
 * storyable.
 *
 * Below `md` the sidebar is replaced by the hamburger + drawer pair;
 * the drawer's open state is local because a persisted overlay is
 * never what the user wants on the next visit.
 *
 * The searchless pages (Settings, Trash, Releases) get no top bar on
 * desktop at all — just the slim mobile bar that keeps the hamburger
 * reachable. `BARLESS_ROUTES` is the list, mapping each to its mobile
 * title.
 *
 * Mounts the suppress-hover-on-scroll hook once for the whole app —
 * keeps the poster grid from churning :hover state while the user
 * scrolls.
 */
/**
 * Routes that render the slim mobile bar instead of the search top
 * bar, and the title each one shows there.
 */
const BARLESS_ROUTES: Record<string, string> = {
  '/settings': 'Settings',
  '/trash': 'Trash',
  '/releases': 'Releases',
};

export function AppShell({ children }: AppShellProps) {
  useSuppressHoverOnScroll();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const barlessTitle = BARLESS_ROUTES[pathname];

  function openDrawer() {
    setDrawerOpen(true);
  }

  return (
    <div className="flex min-h-dvh bg-paper">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        {barlessTitle ? (
          <PageMobileBar
            title={barlessTitle}
            onMenu={openDrawer}
            back={pathname === '/releases' ? <ReleasesBackButton /> : undefined}
            action={pathname === '/releases' ? <ReleasesRefreshButton /> : undefined}
          />
        ) : (
          <TopBar onMenu={openDrawer} />
        )}
        <main className="flex-1">{children}</main>
      </div>
      <MobileDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </div>
  );
}

/**
 * Releases refreshes explicitly — no polling, no realtime. Its own
 * desktop header carries the labelled button, but below `md` the page
 * has no header of its own and this bar is the only chrome there is.
 *
 * The shell owns the bar, so the button announces the request on the
 * window and the route listens, rather than a refresh callback being
 * threaded down through AppShell for one page's sake.
 */
function ReleasesRefreshButton() {
  return (
    <PageBarIconButton
      aria-label="Refresh releases"
      onClick={() => window.dispatchEvent(new Event(RELEASES_REFRESH_EVENT))}
    >
      <MaterialIcon name="refresh" size={18} />
    </PageBarIconButton>
  );
}

/**
 * Back button for the releases detail view, mirroring the back pill the
 * full top bar shows on series detail routes. The open detail lives in
 * the releases view store (the shell owns this bar, so the page can't
 * pass a callback down); nothing renders while the listing is showing.
 */
function ReleasesBackButton() {
  const openInfohash = useReleasesView((s) => s.openInfohash);
  const closeRelease = useReleasesView((s) => s.closeRelease);
  if (!openInfohash) {
    return null;
  }
  return (
    <PageBarIconButton aria-label="Back to attention queue" onClick={closeRelease}>
      <MaterialIcon name="arrow_back" size={18} />
    </PageBarIconButton>
  );
}
