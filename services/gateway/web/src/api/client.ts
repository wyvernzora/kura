import { useSession } from '@/state/session';

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

export function apiErrorMessage(err: unknown, fallback: string): string {
  return err instanceof KuraApiError ? (err.body?.message ?? fallback) : fallback;
}

/**
 * Fetch wrapper used by every data hook. Sends no Authorization —
 * kura does not authenticate, and `credentials: 'include'` carries
 * whatever session cookie the fronting proxy injected. Calls fired
 * before the boot probe settles are refused rather than raced.
 */
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const mode = useSession.getState().mode;
  if (mode !== 'ready') {
    throw new KuraApiError(0, {
      kind: 'precondition',
      message: `api() called before the boot probe settled (mode=${mode})`,
    });
  }

  const headers: HeadersInit = {
    'Content-Type': 'application/json',
    ...(init?.headers ?? {}),
  };
  const res = await fetch(path, {
    ...init,
    credentials: 'include',
    headers,
  });

  // Kura never answers 401, so one can only come from the fronting
  // proxy with an expired session. Reload to let it redirect.
  if (res.status === 401) {
    if (typeof window !== 'undefined') {
      window.location.reload();
    }
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

async function readEnvelope(res: Response): Promise<ApiErrorEnvelope | undefined> {
  try {
    return (await res.json()) as ApiErrorEnvelope;
  } catch {
    return undefined;
  }
}
