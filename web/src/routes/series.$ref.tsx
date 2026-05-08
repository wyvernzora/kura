import { createFileRoute } from '@tanstack/react-router';

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
export const Route = createFileRoute('/series/$ref')({
  component: SeriesDetailRoute,
});

function SeriesDetailRoute() {
  const { ref } = Route.useParams();
  return <SeriesDetail seriesRef={ref} />;
}
