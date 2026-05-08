import type { ReactNode } from 'react';

import { TopBar } from '@/components/TopBar';
import { useSuppressHoverOnScroll } from '@/lib/useSuppressHoverOnScroll';

interface AppShellProps {
  children: ReactNode;
}

/**
 * Layout chrome for authenticated routes. TopBar sits sticky at the
 * top; children fill the remaining viewport. The route tree owns
 * the actual page content via `<Outlet />` (passed in as `children`
 * by the root route) so AppShell stays presentational and storyable.
 *
 * Mounts the suppress-hover-on-scroll hook once for the whole app —
 * keeps the poster grid from churning :hover state while the user
 * scrolls.
 */
export function AppShell({ children }: AppShellProps) {
  useSuppressHoverOnScroll();
  return (
    <div className="flex min-h-dvh flex-col bg-paper">
      <TopBar />
      <main className="flex-1">{children}</main>
    </div>
  );
}
