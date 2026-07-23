import { create } from 'zustand';

/**
 * Auth mode — the state machine documented in scratch/design/web-ui.md
 * (Auth modes + handshake). The two "happy" terminal states are
 * `authenticated-token` (bearer-mode kura) and `authenticated-anon`
 * (KURA_DISABLE_TOKEN or behind an authenticating proxy). The UI
 * distinguishes them so the fetch wrapper knows whether to attach a
 * bearer header, and so destructive ops can hide themselves under a
 * proxy.
 */
export type AuthMode =
  | 'init'
  | 'probe-anon'
  | 'validating-with-token'
  | 'authenticated-token'
  | 'authenticated-anon'
  | 'unauthenticated'
  | 'error';

interface AuthStore {
  mode: AuthMode;
  /** Active bearer token, when mode === 'authenticated-token'. */
  token: string | null;
  /** Last hard error reason ("network", "5xx", …). Drives the error screen. */
  errorReason: string | null;
  /** User-visible message for the current/last login attempt. */
  loginAttemptError: string | null;

  setMode: (mode: AuthMode) => void;
  setToken: (token: string | null) => void;
  setErrorReason: (reason: string | null) => void;
  setLoginAttemptError: (message: string | null) => void;
  signOut: () => void;
}

export const TOKEN_STORAGE_KEY = 'kura.token';

function readStoredToken(): string | null {
  if (typeof sessionStorage === 'undefined') {
    return null;
  }
  try {
    return sessionStorage.getItem(TOKEN_STORAGE_KEY);
  } catch {
    return null;
  }
}

function writeStoredToken(token: string | null): void {
  if (typeof sessionStorage === 'undefined') {
    return;
  }
  try {
    if (token === null || token === '') {
      sessionStorage.removeItem(TOKEN_STORAGE_KEY);
    } else {
      sessionStorage.setItem(TOKEN_STORAGE_KEY, token);
    }
  } catch {
    /* sessionStorage disabled — operate in-memory only. */
  }
}

export const useAuth = create<AuthStore>((set) => ({
  mode: 'init',
  token: readStoredToken(),
  errorReason: null,
  loginAttemptError: null,

  setMode: (mode) => set({ mode }),
  setToken: (token) => {
    writeStoredToken(token);
    set({ token });
  },
  setErrorReason: (reason) => set({ errorReason: reason }),
  setLoginAttemptError: (message) => set({ loginAttemptError: message }),
  signOut: () => {
    writeStoredToken(null);
    set({ token: null, mode: 'unauthenticated', loginAttemptError: null });
  },
}));

/**
 * Convenience predicates for components that just want a yes/no.
 */
export function isAuthenticatedMode(mode: AuthMode): boolean {
  return mode === 'authenticated-token' || mode === 'authenticated-anon';
}

export function isHandshakeMode(mode: AuthMode): boolean {
  return mode === 'init' || mode === 'probe-anon' || mode === 'validating-with-token';
}
