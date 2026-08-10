import { useSession } from '@/state/session';

/**
 * Endpoint the boot probe hits to confirm the API is reachable and is
 * actually kura rather than a proxy interstitial. Lightweight, and the
 * response is useful to warm-cache for chrome.
 */
const PROBE_PATH = '/api/library/v1';

type ProbeReason =
  /** Response shape suggests a proxy intercepted (HTML body, 302). */
  | 'proxy-intercept'
  /** Network failure or unexpected status — no signal. */
  | 'network';

export interface ProbeResult {
  ok: boolean;
  reason?: ProbeReason;
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
  // Kura never answers 401 and never redirects; either means the
  // fronting proxy answered instead of the service.
  if (res.status === 401 || (res.status >= 300 && res.status < 400)) {
    return { ok: false, reason: 'proxy-intercept' };
  }
  return { ok: false, reason: 'network' };
}

export async function probe(): Promise<ProbeResult> {
  try {
    const res = await fetch(PROBE_PATH, {
      method: 'GET',
      credentials: 'include',
      headers: { Accept: 'application/json' },
    });
    return await classifyResponse(res);
  } catch {
    return { ok: false, reason: 'network' };
  }
}

/**
 * Runs the boot probe and settles the session state. A proxy
 * interception reloads the page so the proxy can run its own redirect
 * flow; anything else that isn't a clean JSON 200 is an error screen.
 */
export async function runBootProbe(): Promise<void> {
  const session = useSession.getState();
  const res = await probe();
  if (res.ok) {
    session.setMode('ready');
    return;
  }
  if (res.reason === 'proxy-intercept') {
    reloadForProxy();
    return;
  }
  session.setErrorReason(res.reason ?? 'network');
  session.setMode('error');
}

function reloadForProxy(): void {
  if (typeof window !== 'undefined') {
    window.location.reload();
  }
}
