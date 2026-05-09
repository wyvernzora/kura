import { useMemo } from 'react';

import { useShow } from '@/api/hooks';
import { SeasonPanel } from '@/components/series/SeasonPanel';
import { SeriesDetailSkeleton } from '@/components/series/SeriesDetailSkeleton';
import { SeriesPosterCard } from '@/components/series/SeriesPosterCard';
import { Card } from '@/components/ui/card';
import { cn } from '@/lib/cn';

interface SeriesDetailProps {
  /** Metadata ref (provider:id) from the route params. */
  seriesRef: string | undefined;
}

/**
 * Top-level series detail page. Branches on the `useShow` query state:
 *
 *   - pending  → `SeriesDetailSkeleton`
 *   - error    → centered `Card` with the error message
 *   - success  → poster card + per-season panels
 */
export function SeriesDetail({ seriesRef }: SeriesDetailProps) {
  const { data, isPending, isError, error } = useShow(seriesRef);

  // Specials (season 0) belong at the bottom — they're a footnote,
  // not the headline. Sort regular seasons ascending and append
  // specials last so server ordering stays an implementation detail.
  const orderedSeasons = useMemo(() => {
    if (!data) {
      return [];
    }
    const regular = data.seasons.filter((s) => s.number !== 0).sort((a, b) => a.number - b.number);
    const specials = data.seasons.filter((s) => s.number === 0);
    return [...regular, ...specials];
  }, [data]);

  if (isPending) {
    return <SeriesDetailSkeleton />;
  }
  if (isError || !data) {
    return <ErrorState error={error} />;
  }
  return (
    <div
      className={cn(
        'mx-auto grid max-w-[1920px] gap-9 px-[18px] pt-8 pb-12',
        'md:grid-cols-[300px_1fr]',
      )}
    >
      <SeriesPosterCard show={data} />
      <div className="min-w-0">
        {data.truncated && <TruncatedNotice />}
        {orderedSeasons.length === 0 ? (
          <Card className="p-6 text-sm text-muted">
            No seasons indexed. Run <code>kura scan {data.metadataRef}</code> from the CLI to
            populate.
          </Card>
        ) : (
          orderedSeasons.map((season) => <SeasonPanel key={season.number} season={season} />)
        )}
      </div>
    </div>
  );
}

function ErrorState({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : 'Series not available';
  return (
    <div className="grid place-items-center px-6 py-16">
      <Card className="max-w-md p-6 text-center">
        <h2 className="text-sm font-semibold tracking-tight">Couldn’t load this series</h2>
        <p className="mt-1 text-sm text-muted">{message}</p>
      </Card>
    </div>
  );
}

function TruncatedNotice() {
  return (
    <div
      className={cn(
        'mb-[18px] rounded-[12px] border border-line-soft bg-surface px-4 py-3',
        'text-sm text-ink-2',
      )}
    >
      Episode list truncated by the server. Refresh after a full scan to see the rest.
    </div>
  );
}
