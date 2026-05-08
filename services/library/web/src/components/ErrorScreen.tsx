import { Card } from '@/components/ui/card';

interface ErrorScreenProps {
  reason?: string | null;
  onRetry?: () => void;
}

/**
 * Shown when the boot handshake hits a hard failure (network down,
 * unexpected status). Carries one action — Retry — that re-runs the
 * full app load. We don't auto-retry to avoid hammering an
 * already-struggling server.
 */
export function ErrorScreen({ reason, onRetry }: ErrorScreenProps) {
  const message =
    !reason || reason === 'network'
      ? 'Check that the server is running and reachable on this network.'
      : `Server returned ${reason}.`;
  const handleRetry = onRetry ?? (() => window.location.reload());
  return (
    <main className="grid min-h-dvh place-items-center bg-paper p-6">
      <Card className="w-full max-w-sm p-8">
        <h1 className="text-base font-semibold tracking-tight">Can&apos;t reach kura</h1>
        <p className="mt-2 text-sm text-muted">{message}</p>
        <button
          type="button"
          onClick={handleRetry}
          className="mt-5 inline-flex h-9 w-full items-center justify-center rounded-md bg-ink text-sm font-medium text-paper transition-colors duration-quick ease-out-soft hover:bg-ink/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay"
        >
          Retry
        </button>
      </Card>
    </main>
  );
}
