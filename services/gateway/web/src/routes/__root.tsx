import { createRootRoute, Outlet } from '@tanstack/react-router';
import { useEffect } from 'react';

import { runBootProbe } from '@/api/probe';
import { AppShell } from '@/components/AppShell';
import { ErrorScreen } from '@/components/ErrorScreen';
import { Splash } from '@/components/Splash';
import { useSession } from '@/state/session';

export const Route = createRootRoute({
  component: RootLayout,
});

/**
 * Top-level layout. Runs the boot probe on first mount and branches
 * the rendered tree on the resulting state:
 *
 *   init  → Splash
 *   error → ErrorScreen
 *   ready → AppShell + <Outlet />
 *
 * Routes only render once the probe has settled; the fetch wrapper in
 * src/api/client.ts also refuses to run before that, so data hooks
 * can't accidentally fire during boot.
 */
function RootLayout() {
  const mode = useSession((s) => s.mode);
  const errorReason = useSession((s) => s.errorReason);

  useEffect(() => {
    if (mode === 'init') {
      void runBootProbe();
    }
  }, [mode]);

  if (mode === 'init') {
    return <Splash />;
  }
  if (mode === 'error') {
    return <ErrorScreen reason={errorReason} />;
  }
  return (
    <AppShell>
      <Outlet />
    </AppShell>
  );
}
