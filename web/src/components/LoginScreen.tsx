import { type FormEvent, useEffect, useRef, useState } from 'react';

import { attemptLogin } from '@/api/probe';
import { Card } from '@/components/ui/card';
import { useAuth } from '@/state/auth';

interface LoginScreenViewProps {
  error?: string;
  submitting?: boolean;
  onSubmit: (token: string) => void;
}

/**
 * Presentational login screen — pure props in, pure callback out.
 * Storybook drives this directly to exercise the visual states
 * without depending on the auth store. The connected `LoginScreen`
 * below wires it up to attemptLogin.
 */
export function LoginScreenView({ error, submitting, onSubmit }: LoginScreenViewProps) {
  const [token, setToken] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);
  const trimmed = token.trim();
  const disabled = submitting || trimmed.length === 0;

  useEffect(() => {
    // Focus on mount — dedicated login screen, single form. We hop
    // through useEffect rather than the autoFocus attribute so the
    // a11y lint stays clean and so the focus survives Storybook
    // remounts that don't run the autoFocus path.
    inputRef.current?.focus();
  }, []);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (submitting) {
      return;
    }
    onSubmit(token);
  };

  return (
    <main className="grid min-h-dvh place-items-center bg-paper p-6">
      <Card className="w-full max-w-sm p-7">
        <header className="flex items-center gap-3">
          <div
            aria-hidden="true"
            className="grid h-10 w-10 place-items-center rounded-md bg-ink font-serif text-xl font-semibold text-paper"
          >
            K
          </div>
          <div>
            <h1 className="text-base font-semibold tracking-tight">Sign in to kura</h1>
            <p className="text-xs text-muted">Enter the bearer token from your server.</p>
          </div>
        </header>

        <form className="mt-6 space-y-3" onSubmit={handleSubmit}>
          <label className="block">
            <span className="sr-only">Bearer token</span>
            <input
              ref={inputRef}
              type="password"
              autoComplete="off"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              disabled={submitting}
              placeholder="Bearer token"
              className="h-9 w-full rounded-md border border-line-soft bg-surface-2 px-3 text-sm text-ink placeholder:text-muted focus:border-line focus:bg-surface focus:outline-none focus:ring-2 focus:ring-overlay disabled:opacity-50"
            />
          </label>

          {error && (
            <div
              role="alert"
              className="rounded-md bg-status-error/10 px-3 py-2 text-xs text-status-error"
            >
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={disabled}
            className="inline-flex h-9 w-full items-center justify-center rounded-md bg-ink text-sm font-medium text-paper transition-colors duration-quick ease-out-soft hover:bg-ink/90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay disabled:pointer-events-none disabled:opacity-50"
          >
            {submitting ? 'Signing in…' : 'Sign in'}
          </button>
        </form>

        <footer className="mt-5 border-t border-line-soft pt-4 text-xs text-muted">
          <p>
            Find your token at <code className="font-mono text-ink-2">/var/lib/kura/token</code> on
            the server, or set <code className="font-mono text-ink-2">KURA_TOKEN</code> in the
            environment.
          </p>
        </footer>
      </Card>
    </main>
  );
}

/**
 * Connected login screen. Subscribes to auth-store fields it needs and
 * dispatches the login attempt. Used by the root layout when
 * mode === 'unauthenticated'.
 */
export function LoginScreen() {
  const error = useAuth((s) => s.loginAttemptError) ?? undefined;
  const mode = useAuth((s) => s.mode);
  return (
    <LoginScreenView
      error={error}
      submitting={mode === 'validating-with-token'}
      onSubmit={(token) => {
        void attemptLogin(token);
      }}
    />
  );
}
