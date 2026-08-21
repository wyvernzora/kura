import {
  FIXTURE_QUEUE_STATS,
  FIXTURE_RELEASE_SERIES,
  releaseDetailFixture,
  releasePage,
} from '../src/components/_releaseFixtures';
import { trashListFixture } from '../src/components/_trashFixtures';

/**
 * Storybook-only fetch mock for the kura REST surface. Stories don't
 * have a real backend, so unmocked POSTs to `/api/library/v1/series/*\/scan`
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
      const query = new URL(url, window.location.origin).searchParams;

      // GET /api/library/v1/series → the library the releases page joins
      // refs against and searches for hand-match candidates. A story can
      // seed `window.__kuraSeriesList__` to narrow or empty it.
      if (method === 'GET' && /\/api\/library\/v1\/series(\?.*)?$/.test(url)) {
        return jsonResponse({ items: w.__kuraSeriesList__ ?? FIXTURE_RELEASE_SERIES });
      }

      // POST /api/library/v1/series/resolve → echo the requested ref back
      // as a single candidate, the unique-match cardinality. A story
      // seeds `window.__kuraResolve__` for the failure paths.
      if (method === 'POST' && /\/api\/library\/v1\/series\/resolve$/.test(url)) {
        const seeded = w.__kuraResolve__;
        if (seeded?.status && seeded.status >= 400) {
          return jsonResponse(seeded.body, seeded.status);
        }
        if (seeded) {
          return jsonResponse(seeded);
        }
        let ref = '';
        try {
          const body = JSON.parse(String(init?.body ?? '{}')) as { terms?: string[] };
          ref = body.terms?.[0] ?? '';
        } catch {
          // fall through to the empty-candidates response
        }
        const row = FIXTURE_RELEASE_SERIES.find((series) => series.ref === ref);
        return jsonResponse({
          candidates: row
            ? [
                {
                  ref: row.ref,
                  preferredTitle: row.title,
                  canonicalTitle: row.canonicalTitle,
                  year: 2023,
                },
              ]
            : [],
        });
      }

      // GET /api/releases/v1 → one page of the release fixtures, filtered
      // the way the endpoint filters (status, maxConfidence, cursor).
      if (method === 'GET' && /\/api\/releases\/v1(\?.*)?$/.test(url)) {
        return jsonResponse(
          releasePage(
            {
              status: query.get('status'),
              maxConfidence: query.get('maxConfidence'),
              cursor: query.get('cursor'),
              limit: query.get('limit'),
            },
            w.__kuraReleases__,
          ),
        );
      }

      if (method === 'GET' && /\/api\/releases\/v1\/queue\/stats$/.test(url)) {
        return jsonResponse(FIXTURE_QUEUE_STATS);
      }

      // PUT /api/releases/v1/{infohash}/status → the outcome a story asked
      // for via `window.__kuraSetStatus__`, defaulting to acceptance.
      if (method === 'PUT' && /\/api\/releases\/v1\/[^/]+\/status$/.test(url)) {
        const outcome = w.__kuraSetStatus__;
        if (outcome?.status && outcome.status >= 400) {
          return jsonResponse(outcome.body, outcome.status);
        }
        return jsonResponse({ ok: true });
      }

      if (method === 'GET' && /\/api\/releases\/v1\/[^/?]+$/.test(url)) {
        const infohash = url.split('/').pop() ?? '';
        const detail = releaseDetailFixture(decodeURIComponent(infohash));
        return detail
          ? jsonResponse(detail)
          : jsonResponse({ kind: 'not_found', message: `no release ${infohash}` }, 404);
      }

      // GET /api/library/v1/trash → the shared trash fixtures, so the
      // /trash stories render a realistic listing. A story can override
      // the payload (empty trash, a specific failure) by seeding
      // `window.__kuraTrashList__` before it mounts.
      if (method === 'GET' && /\/api\/library\/v1\/trash$/.test(url)) {
        return jsonResponse(w.__kuraTrashList__ ?? trashListFixture());
      }

      // DELETE on either trash route → the outcome a story asked for
      // via `window.__kuraTrashEmpty__`, defaulting to a plausible
      // success so clicking through the confirm gate lands somewhere.
      if (method === 'DELETE' && /\/api\/library\/v1\/(series\/.+\/)?trash(\?.*)?$/.test(url)) {
        const outcome = w.__kuraTrashEmpty__;
        if (outcome?.status && outcome.status >= 400) {
          return jsonResponse(outcome.body, outcome.status);
        }
        return jsonResponse(
          outcome ?? {
            series: [],
            totalEntries: 12,
            attempts: 12,
            reclaimedBytes: 18_400_000_000,
          },
        );
      }

      // PATCH /api/library/v1/series/{ref}/tags → echo the additive
      // expressions back as the stored tag set so the settings modal's
      // mutation succeeds instead of 404ing when a story is clicked.
      if (method === 'PATCH' && /\/api\/library\/v1\/series\/.+\/tags$/.test(url)) {
        const ref = decodeURIComponent(url.split('/').slice(-2)[0] ?? '');
        let tags: string[] = [];
        try {
          const body = JSON.parse(String(init?.body ?? '{}')) as { tags?: string[] };
          tags = (body.tags ?? []).filter((t) => !t.startsWith('!'));
        } catch {
          // keep empty tag set on unparsable body
        }
        return jsonResponse({ ref, tags });
      }

      // POST /api/library/v1/series/{ref}/scan → return a job handle ack.
      if (method === 'POST' && /\/api\/library\/v1\/series\/.+\/scan$/.test(url)) {
        const jobId = `sb-mock-${Math.random().toString(36).slice(2, 10)}`;
        return jsonResponse(
          {
            jobId,
            kind: 'scan',
            statusURL: `/api/library/v1/jobs/${jobId}`,
            streamURL: `/api/library/v1/jobs/${jobId}/stream`,
            submittedAt: new Date().toISOString(),
          },
          202,
        );
      }

      // POST /api/library/v1/scan and /api/library/v1/reindex → job ack.
      if (method === 'POST' && /\/api\/library\/v1\/(scan|reindex)$/.test(url)) {
        const kind = url.endsWith('/scan') ? 'scan_all' : 'reindex';
        const jobId = `sb-mock-${Math.random().toString(36).slice(2, 10)}`;
        return jsonResponse(
          {
            jobId,
            kind,
            statusURL: `/api/library/v1/jobs/${jobId}`,
            streamURL: `/api/library/v1/jobs/${jobId}/stream`,
            submittedAt: new Date().toISOString(),
          },
          202,
        );
      }

      // GET /api/library/v1/jobs/{id} → if a story has seeded `kura.libraryJob`
      // pointing at this jobId, keep returning a running state with
      // synthetic progress so the gear-menu running view stays visible
      // for the snapshot. Otherwise fall through to the default
      // terminal-succeeded shape that lets idle stories complete the
      // kickoff cycle without lingering.
      if (method === 'GET' && /\/api\/library\/v1\/jobs\/[^/]+$/.test(url)) {
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
