/**
 * Storybook-only fetch mock for the kura REST surface. Stories don't
 * have a real backend, so unmocked POSTs to `/api/v1/series/*\/scan`
 * 404 and the scan hook flips to its kickoff-error state. We intercept
 * the two endpoints the scan flow uses and return canned responses so
 * clicking a story button exercises the full kickoff → poll → terminal
 * cycle visually.
 *
 * Pass-through: any URL that doesn't match the kura API patterns falls
 * back to the real fetch (font CDN, image hosts, etc.).
 *
 * One-shot install — Vite re-evaluates this module on HMR but the
 * `__kuraFetchMockInstalled__` guard keeps the original `fetch` from
 * being clobbered by a wrapper-of-wrapper-of-wrapper.
 */

if (typeof window !== 'undefined') {
  // biome-ignore lint/suspicious/noExplicitAny: globalThis flag for HMR-safe install
  const w = window as any;
  if (!w.__kuraFetchMockInstalled__) {
    w.__kuraFetchMockInstalled__ = true;
    const realFetch = window.fetch.bind(window);

    function urlOf(input: RequestInfo | URL): string {
      if (typeof input === 'string') {
        return input;
      }
      if (input instanceof URL) {
        return input.toString();
      }
      return input.url;
    }

    function jsonResponse(body: unknown, status = 200): Response {
      return new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      });
    }

    window.fetch = async (input, init) => {
      const url = urlOf(input);
      const method = (init?.method ?? 'GET').toUpperCase();

      // POST /api/v1/series/{ref}/scan → return a job handle ack.
      if (method === 'POST' && /\/api\/v1\/series\/.+\/scan$/.test(url)) {
        const jobId = `sb-mock-${Math.random().toString(36).slice(2, 10)}`;
        return jsonResponse(
          {
            jobId,
            kind: 'scan',
            statusURL: `/api/v1/jobs/${jobId}`,
            streamURL: `/api/v1/jobs/${jobId}/stream`,
            submittedAt: new Date().toISOString(),
          },
          202,
        );
      }

      // GET /api/v1/jobs/{id} → terminal succeeded with empty result.
      // The hook treats empty `skipped` as a clean run and clears the
      // record, returning to idle. Story state machine is exercised
      // end-to-end without lingering in a fake running phase.
      if (method === 'GET' && /\/api\/v1\/jobs\/[^/]+$/.test(url)) {
        return jsonResponse({
          jobId: 'sb-mock',
          kind: 'scan',
          state: 'succeeded',
          startedAt: new Date(Date.now() - 1500).toISOString(),
          endedAt: new Date().toISOString(),
          result: { synced: [], skipped: [], orphanSlots: null },
        });
      }

      return realFetch(input, init);
    };
  }
}
