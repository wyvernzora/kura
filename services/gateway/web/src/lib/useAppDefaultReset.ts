import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useCallback } from 'react';

import { useLibraryFilters } from '@/state/library';
import { useSearch } from '@/state/search';

/**
 * "Take me back to the app default view." Navigates to `/`, clears the
 * library filters + sort, clears the search session, and — when the
 * user is already on `/`, where there is no route change to trigger
 * the router's scroll restoration — scrolls the window to the top.
 *
 * Shared by every affordance that promises that reset: the kura mark
 * in the sidebar and the mobile drawer, and the Library nav item
 * (the library's feature default *is* the app default, so clicking it
 * while already on `/` still resets rather than doing nothing).
 */
export function useAppDefaultReset(): () => void {
  const navigate = useNavigate();
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  return useCallback(() => {
    useLibraryFilters.getState().clear();
    useSearch.getState().clear();
    if (pathname === '/') {
      // `auto` matches the browser's default snap rather than
      // animating a long smooth-scroll for users far down the grid.
      window.scrollTo({ top: 0, behavior: 'auto' });
      return;
    }
    void navigate({ to: '/' });
  }, [navigate, pathname]);
}
