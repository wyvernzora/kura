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

      // PATCH /api/v1/series/{ref}/tags → echo the additive expressions
      // back as the stored tag set so the settings modal's mutation
      // succeeds instead of 404ing when a story is clicked.
      if (method === 'PATCH' && /\/api\/v1\/series\/.+\/tags$/.test(url)) {
        const ref = decodeURIComponent(url.split('/').slice(-2)[0] ?? '');
        let tags: string[] = [];
        try {
          const body = JSON.parse(String(init?.body ?? '{}')) as { tags?: string[] };
          tags = (body.tags ?? []).filter((t) => !t.startsWith('!'));
        } catch {
          // keep empty tag set on unparsable body
        }
        return jsonResponse({ metadataRef: ref, tags });
      }

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

      // POST /api/v1/library/scan and /reindex → job ack.
      if (method === 'POST' && /\/api\/v1\/library\/(scan|reindex)$/.test(url)) {
        const kind = url.endsWith('/scan') ? 'scan_all' : 'reindex';
        const jobId = `sb-mock-${Math.random().toString(36).slice(2, 10)}`;
        return jsonResponse(
          {
            jobId,
            kind,
            statusURL: `/api/v1/jobs/${jobId}`,
            streamURL: `/api/v1/jobs/${jobId}/stream`,
            submittedAt: new Date().toISOString(),
          },
          202,
        );
      }

      // GET /api/v1/jobs/{id} → if a story has seeded `kura.libraryJob`
      // pointing at this jobId, keep returning a running state with
      // synthetic progress so the gear-menu running view stays visible
      // for the snapshot. Otherwise fall through to the default
      // terminal-succeeded shape that lets idle stories complete the
      // kickoff cycle without lingering.
      if (method === 'GET' && /\/api\/v1\/jobs\/[^/]+$/.test(url)) {
        const id = url.split('/').pop() ?? '';
        const libraryRecord = readLibraryJobRecord();
        if (libraryRecord && libraryRecord.jobId === id) {
          return jsonResponse({
            jobId: id,
            kind: libraryRecord.kind === 'reindex' ? 'reindex' : 'scan_all',
            state: 'running',
            startedAt: libraryRecord.startedAt,
            progress: {
              status: 'update',
              stage: libraryRecord.kind === 'reindex' ? 'reindex' : 'scan_all',
              message: 'Frieren — Beyond Journey’s End',
              current: 312,
              total: 742,
            },
          });
        }
        return jsonResponse({
          jobId: id,
          kind: 'scan',
          state: 'succeeded',
          startedAt: new Date(Date.now() - 1500).toISOString(),
          endedAt: new Date().toISOString(),
          result: { synced: [], skipped: [], orphanSlots: null },
        });
      }

      return realFetch(input, init);
    };

    function readLibraryJobRecord(): { kind: string; jobId: string; startedAt: string } | null {
      try {
        const raw = window.localStorage.getItem('kura.libraryJob');
        if (!raw) {
          return null;
        }
        return JSON.parse(raw);
      } catch {
        return null;
      }
    }
  }
}
