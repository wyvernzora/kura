interface SplashProps {
  message?: string;
}

/**
 * Quiet boot screen shown during init / probe-anon /
 * validating-with-token. The pulsing dot is intentional — it tells
 * the user something is happening without claiming progress we can't
 * actually report.
 */
export function Splash({ message }: SplashProps) {
  return (
    <main className="grid min-h-dvh place-items-center bg-paper">
      <div className="flex items-center gap-3 text-sm text-muted">
        <span aria-hidden="true" className="h-2 w-2 animate-pulse rounded-full bg-status-airing" />
        {message ?? 'Connecting to kura…'}
      </div>
    </main>
  );
}
