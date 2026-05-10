import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createRouter } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { routeTree } from './routeTree.gen';
import './styles/globals.css';

// scrollRestoration: true tells TanStack Router to track per-route
// scroll positions in sessionStorage and restore them on browser-back
// navigation. Without this, navigating from `/series/$ref` back to
// `/` lands the user at the top of the grid instead of where they
// left off. Library uses the window scroll (useWindowVirtualizer in
// VirtualPosterGrid), so the default window-level scroll element is
// what gets restored.
const router = createRouter({ routeTree, scrollRestoration: true });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
    },
  },
});

const rootElement = document.getElementById('root');
if (!rootElement) {
  throw new Error('root element not found');
}

createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
