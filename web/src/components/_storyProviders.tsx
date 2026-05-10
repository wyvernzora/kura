import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement, ReactNode } from 'react';

import { useAuth } from '@/state/auth';

/**
 * Stories that exercise hooks built on top of TanStack Query (`useShow`,
 * `useScanJob`, etc.) need a QueryClient in context. They also call
 * `api()` which gates on the auth-mode state machine: outside an
 * authenticated mode, `api()` rejects synchronously with a precondition
 * error. Stories want the realistic visual state, not the error state,
 * so we lock the auth store into `authenticated-anon` for the duration
 * of Storybook.
 *
 * The QueryClient retries are disabled so a story-level fetch failure
 * stays visible in one render rather than churning behind a spinner.
 */
function makeQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
      mutations: { retry: false },
    },
  });
}

export function StoryProviders({ children }: { children: ReactNode }) {
  // Pin the auth store the first time a provider tree mounts. Storybook
  // remounts decorators; idempotent setMode is fine.
  if (useAuth.getState().mode !== 'authenticated-anon') {
    useAuth.getState().setMode('authenticated-anon');
  }
  return <QueryClientProvider client={makeQueryClient()}>{children}</QueryClientProvider>;
}

/** Convenience decorator wrapping the story tree in `StoryProviders`. */
export function withStoryProviders(Story: () => ReactElement) {
  return (
    <StoryProviders>
      <Story />
    </StoryProviders>
  );
}
