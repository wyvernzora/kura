import { createRootRoute, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';

import { performHandshake } from '@/api/probe';
import { AppShell } from '@/components/AppShell';
import { ErrorScreen } from '@/components/ErrorScreen';
import { LoginScreen } from '@/components/LoginScreen';
import { Splash } from '@/components/Splash';
import { isHandshakeMode, useAuth } from '@/state/auth';

export const Route = createRootRoute({
  component: RootLayout,
});

/**
 * Top-level layout. Drives the auth handshake on first mount and
 * branches the rendered tree on the resulting state:
 *
 *   handshake (init / probe-anon / validating-with-token) → Splash
 *   error                                                  → ErrorScreen
 *   unauthenticated                                        → LoginScreen
 *   authenticated-{token,anon}                             → AppShell + <Outlet />
 *
 * Routes only render once we're in an authenticated mode; the fetch
 * wrapper in src/api/client.ts also refuses to run before that, so
 * data hooks can't accidentally fire during the handshake.
 */
function RootLayout() {
  const mode = useAuth((s) => s.mode);
  const errorReason = useAuth((s) => s.errorReason);

  useEffect(() => {
    if (mode === 'init') {
      void performHandshake();
    }
  }, [mode]);

  if (isHandshakeMode(mode)) {
    return <Splash />;
  }
  if (mode === 'error') {
    return <ErrorScreen reason={errorReason} />;
  }
  if (mode === 'unauthenticated') {
    return <LoginScreen />;
  }
  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}
