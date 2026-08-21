import { useRouterState } from '@tanstack/react-router';
import { type ReactNode, useState } from 'react';

import { MobileDrawer } from '@/components/MobileDrawer';
import { Sidebar } from '@/components/Sidebar';
import { SettingsMobileBar, TopBar } from '@/components/TopBar';
import { useSuppressHoverOnScroll } from '@/lib/useSuppressHoverOnScroll';

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
 * Settings has no search, so it gets no top bar on desktop at all —
 * just the slim mobile bar that keeps the hamburger reachable.
 *
 * Mounts the suppress-hover-on-scroll hook once for the whole app —
 * keeps the poster grid from churning :hover state while the user
 * scrolls.
 */
export function AppShell({ children }: AppShellProps) {
  useSuppressHoverOnScroll();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const onSettings = pathname === '/settings';

  function openDrawer() {
    setDrawerOpen(true);
  }

  return (
    <div className="flex min-h-dvh bg-paper">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col">
        {onSettings ? <SettingsMobileBar onMenu={openDrawer} /> : <TopBar onMenu={openDrawer} />}
        <main className="flex-1">{children}</main>
      </div>
      <MobileDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </div>
  );
}
