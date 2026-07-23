import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';

import { SeriesDetail } from '@/components/series/SeriesDetail';

/**
 * Series detail route. `$ref` is the metadata ref (provider:id, e.g.
 * `tvdb:370070`) — the same identifier the library list emits as
 * `ListRow.metadataRef`. Untracked rows have no metadata ref so the
 * library home does not link them.
 *
 * Loading / error branching lives in `SeriesDetail`; the route stays
 * trivial so file-based routing stays the source of truth.
 */
/**
 * `?preview=true` renders the page from live provider metadata for a
 * series not yet in the library (see `SeriesDetail` / the "Not in your
 * library" search section). Omitted for tracked series. Only `true` is
 * carried in the URL; anything else drops the param.
 */
interface SeriesSearch {
  preview?: true;
}

export const Route = createFileRoute('/series/$ref')({
  validateSearch: (search: Record<string, unknown>): SeriesSearch =>
    search.preview === true || search.preview === 'true' ? { preview: true } : {},
  component: SeriesDetailRoute,
});

function SeriesDetailRoute() {
  const { ref } = Route.useParams();
  const { preview } = Route.useSearch();
  const navigate = useNavigate();

  // Escape returns to the library grid — the same destination as the
  // top-bar back button. Skipped while typing, and when a dialog or
  // menu is open (those own Escape to dismiss themselves first).
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== 'Escape' || e.defaultPrevented) {
        return;
      }
      const el = document.activeElement;
      if (
        el instanceof HTMLElement &&
        (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)
      ) {
        return;
      }
      if (document.querySelector('[role="dialog"],[role="menu"]')) {
        return;
      }
      void navigate({ to: '/' });
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navigate]);

  return <SeriesDetail seriesRef={ref} preview={preview ?? false} />;
}
