import { create } from 'zustand';

/**
 * Boot state. Kura itself does not authenticate — the Pomerium
 * boundary in front of the deployment is the only gate — so this is
 * purely "have we confirmed the API is reachable and speaking JSON
 * yet", which the fetch wrapper uses to refuse calls fired during
 * boot.
 */
export type SessionMode = 'init' | 'ready' | 'error';

interface SessionStore {
  mode: SessionMode;
  /** Last hard error reason ("network", "5xx", …). Drives the error screen. */
  errorReason: string | null;

  setMode: (mode: SessionMode) => void;
  setErrorReason: (reason: string | null) => void;
}

export const useSession = create<SessionStore>((set) => ({
  mode: 'init',
  errorReason: null,

  setMode: (mode) => set({ mode }),
  setErrorReason: (reason) => set({ errorReason: reason }),
}));
