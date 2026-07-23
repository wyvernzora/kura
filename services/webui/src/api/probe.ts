import { useAuth } from '@/state/auth';

import { type ApiErrorEnvelope, ApiErrorKinds } from './types';

/**
 * Endpoint the auth handshake hits to classify the deployment mode.
 * Bearer-protected (so a 401 here distinguishes bearer-required
 * deployments from anon ones) and lightweight (the response is small
 * and useful to warm-cache for chrome).
 *
 * `/api/v1/health` is intentionally NOT used: it's bearer-exempt by
 * design, so a 200 there is uninformative about token state.
 */
const PROBE_PATH = '/api/v1/library';

type ProbeReason =
  /** Real kura returned 401 with its error envelope (or a Bearer challenge). */
  | 'kura-401'
  /** Response shape suggests a proxy intercepted (HTML body, 302, 401 with no envelope). */
  | 'proxy-intercept'
  /** Network failure or unexpected status — no signal. */
  | 'network';

export interface ProbeResult {
  ok: boolean;
  reason?: ProbeReason;
}

async function rawFetch(url: string, headers?: HeadersInit): Promise<Response> {
  return fetch(url, {
    method: 'GET',
    credentials: 'include',
    headers: { Accept: 'application/json', ...(headers ?? {}) },
  });
}

async function readErrorEnvelope(res: Response): Promise<ApiErrorEnvelope | undefined> {
  try {
    const json = (await res.json()) as ApiErrorEnvelope;
    if (json && typeof json.kind === 'string' && typeof json.message === 'string') {
      return json;
    }
  } catch {
    /* not JSON */
  }
  return undefined;
}

async function classifyResponse(res: Response): Promise<ProbeResult> {
  if (res.status === 200) {
    const contentType = res.headers.get('Content-Type') ?? '';
    if (!contentType.toLowerCase().startsWith('application/json')) {
      return { ok: false, reason: 'proxy-intercept' };
    }
    try {
      await res.json();
    } catch {
      return { ok: false, reason: 'proxy-intercept' };
    }
    return { ok: true };
  }
  if (res.status === 401) {
    const envelope = await readErrorEnvelope(res);
    const wwwAuth = res.headers.get('WWW-Authenticate')?.toLowerCase() ?? '';
    if (envelope?.kind === ApiErrorKinds.Unauthorized || wwwAuth.includes('bearer')) {
      return { ok: false, reason: 'kura-401' };
    }
    return { ok: false, reason: 'proxy-intercept' };
  }
  if (res.status === 302 || (res.status >= 300 && res.status < 400)) {
    return { ok: false, reason: 'proxy-intercept' };
  }
  return { ok: false, reason: 'network' };
}

export async function probeAnon(): Promise<ProbeResult> {
  try {
    const res = await rawFetch(PROBE_PATH);
    return await classifyResponse(res);
  } catch {
    return { ok: false, reason: 'network' };
  }
}

export async function probeWithToken(token: string): Promise<ProbeResult> {
  try {
    const res = await rawFetch(PROBE_PATH, { Authorization: `Bearer ${token}` });
    return await classifyResponse(res);
  } catch {
    return { ok: false, reason: 'network' };
  }
}

/**
 * Runs the boot-time handshake. Tries (in order): a `?token=...` URL
 * param (bootstrap link printed by `kura serve`), the saved
 * sessionStorage token, then anon. End states map to the auth state
 * machine documented in scratch/design/web-ui.md.
 *
 * URL token is scrubbed from `window.location` immediately after it's
 * been read so it doesn't survive into browser history, link copies,
 * or referrer headers.
 */
export async function performHandshake(): Promise<void> {
  const auth = useAuth.getState();

  const urlToken = consumeUrlToken();
  if (urlToken) {
    auth.setMode('validating-with-token');
    const res = await probeWithToken(urlToken);
    if (res.ok) {
      auth.setToken(urlToken);
      auth.setMode('authenticated-token');
      return;
    }
    // URL token failed — fall through to the regular flow rather
    // than surfacing an error. The user could be reusing a stale
    // bootstrap link against a freshly-restarted server.
    if (res.reason === 'proxy-intercept') {
      reloadForProxy();
      return;
    }
  }

  const stored = auth.token;
  if (stored) {
    auth.setMode('validating-with-token');
    const res = await probeWithToken(stored);
    if (res.ok) {
      auth.setMode('authenticated-token');
      return;
    }
    if (res.reason === 'kura-401') {
      auth.setToken(null);
      // Fall through to anon probe — the bearer is stale, but the
      // server might be in disabled-token mode now.
    } else if (res.reason === 'proxy-intercept') {
      reloadForProxy();
      return;
    } else {
      auth.setErrorReason(res.reason ?? 'network');
      auth.setMode('error');
      return;
    }
  }

  auth.setMode('probe-anon');
  const anon = await probeAnon();
  if (anon.ok) {
    auth.setMode('authenticated-anon');
    return;
  }
  if (anon.reason === 'kura-401') {
    auth.setMode('unauthenticated');
    return;
  }
  if (anon.reason === 'proxy-intercept') {
    reloadForProxy();
    return;
  }
  auth.setErrorReason(anon.reason ?? 'network');
  auth.setMode('error');
}

/**
 * Reads `?token=...` from window.location.search if present, then
 * scrubs the param from the URL via `history.replaceState` so it
 * doesn't end up in browser history or copy-link operations.
 *
 * Returns the token string or null. Empty / whitespace-only values
 * are treated as absent.
 */
function consumeUrlToken(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  let token: string | null = null;
  try {
    const params = new URLSearchParams(window.location.search);
    const raw = params.get('token');
    if (raw && raw.trim().length > 0) {
      token = raw.trim();
    }
  } catch {
    return null;
  }
  if (token !== null) {
    try {
      const url = new URL(window.location.href);
      url.searchParams.delete('token');
      const search = url.searchParams.toString();
      const next = `${url.pathname}${search ? `?${search}` : ''}${url.hash}`;
      window.history.replaceState(window.history.state, '', next);
    } catch {
      /* history API unavailable — best effort */
    }
  }
  return token;
}

/**
 * Validates the user's bearer entry from the login screen. On success
 * persists the token + transitions to authenticated-token. Failure
 * branches surface a user-visible message via `loginAttemptError`.
 */
export async function attemptLogin(rawToken: string): Promise<void> {
  const token = rawToken.trim();
  const auth = useAuth.getState();
  auth.setLoginAttemptError(null);

  if (!token) {
    auth.setLoginAttemptError('Enter a bearer token to sign in.');
    return;
  }

  auth.setMode('validating-with-token');
  const res = await probeWithToken(token);

  if (res.ok) {
    auth.setToken(token);
    auth.setMode('authenticated-token');
    return;
  }
  if (res.reason === 'kura-401') {
    auth.setMode('unauthenticated');
    auth.setLoginAttemptError('That token doesn’t match. Try again.');
    return;
  }
  if (res.reason === 'proxy-intercept') {
    reloadForProxy();
    return;
  }
  auth.setMode('unauthenticated');
  auth.setLoginAttemptError('Can’t reach kura. Check the server is running.');
}

function reloadForProxy(): void {
  if (typeof window !== 'undefined') {
    window.location.reload();
  }
}
