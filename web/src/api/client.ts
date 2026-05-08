import { useAuth } from '@/state/auth';

import type { ApiErrorEnvelope } from './types';

/**
 * Thrown by `api()` for any non-2xx response. `body` is the parsed
 * kura error envelope when the server speaks JSON; `undefined` when
 * the response wasn't kura-shaped (e.g. a proxy returned HTML).
 */
export class KuraApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly body: ApiErrorEnvelope | undefined,
  ) {
    super(body?.message ?? `HTTP ${status}`);
    this.name = 'KuraApiError';
  }
}

/**
 * Mode-aware fetch wrapper used by every data hook.
 *
 *   - In `authenticated-token` mode it attaches `Authorization: Bearer ...`.
 *   - In `authenticated-anon` mode it sends no Authorization, relying
 *     on `credentials: 'include'` for proxy-injected session cookies.
 *   - All other modes (init / probe / unauthenticated / error) should
 *     not be calling api() — a runtime check guards against early use.
 *
 * 401 handling is centralized: in token mode we drop the stale token
 * and route the user back to the login screen via the auth store; in
 * anon mode we reload the page so the proxy can run its redirect
 * dance.
 */
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const auth = useAuth.getState();
  if (!isCallableMode(auth.mode)) {
    throw new KuraApiError(0, {
      kind: 'precondition',
      message: `api() called before authentication completed (mode=${auth.mode})`,
    });
  }

  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(init?.headers ?? {}),
  };
  if (auth.mode === 'authenticated-token' && auth.token) {
    (headers as Record<string, string>).Authorization = `Bearer ${auth.token}`;
  }

  const res = await fetch(path, {
    ...init,
    credentials: 'include',
    headers,
  });

  if (res.status === 401) {
    handleUnauthorized();
    throw new KuraApiError(401, await readEnvelope(res));
  }
  if (!res.ok) {
    throw new KuraApiError(res.status, await readEnvelope(res));
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

function isCallableMode(mode: ReturnType<typeof useAuth.getState>['mode']): boolean {
  return mode === 'authenticated-token' || mode === 'authenticated-anon';
}

async function readEnvelope(res: Response): Promise<ApiErrorEnvelope | undefined> {
  try {
    return (await res.json()) as ApiErrorEnvelope;
  } catch {
    return undefined;
  }
}

function handleUnauthorized(): void {
  const auth = useAuth.getState();
  if (auth.mode === 'authenticated-token') {
    auth.setToken(null);
    auth.setMode('unauthenticated');
    return;
  }
  if (auth.mode === 'authenticated-anon' && typeof window !== 'undefined') {
    window.location.reload();
  }
}
