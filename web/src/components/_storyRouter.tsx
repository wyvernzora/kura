import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router';
import type { ReactNode } from 'react';

/**
 * Decorator helper for stories that render TanStack Router primitives
 * (`<Link>`, `useNavigate`, `useRouterState`, etc.). Spins up a tiny
 * memory router with stub `/` and `/series/$ref` routes that just
 * render the story content. The production routeTree is intentionally
 * not wired here — story isolation is the point.
 *
 * Pass `initialPath` (default `/`) to seed the memory history at a
 * specific route so route-aware components render their detail-mode
 * variant for review.
 *
 * Usage:
 *
 * ```tsx
 * decorators: [(Story) => <StoryRouter><Story /></StoryRouter>]
 * ```
 */
export function StoryRouter({
  children,
  initialPath = '/',
}: {
  children: ReactNode;
  initialPath?: string;
}) {
  const rootRoute = createRootRoute();
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: () => <>{children}</>,
  });
  const seriesRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/series/$ref',
    component: () => <>{children}</>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute, seriesRoute]),
    history: createMemoryHistory({ initialEntries: [initialPath] }),
  });
  return <RouterProvider router={router} />;
}
