import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useCallback, useMemo, useState } from 'react';

import { useSeriesList } from '@/api/hooks';
import type { ListRow } from '@/api/types';
import { ClearFiltersButton } from '@/components/ClearFiltersButton';
import { SortDropdown } from '@/components/SortDropdown';
import { StatusFilterDropdown } from '@/components/StatusFilterDropdown';
import { ValueFilterDropdown } from '@/components/ValueFilterDropdown';
import { VirtualPosterGrid } from '@/components/VirtualPosterGrid';
import { Card } from '@/components/ui/card';
import {
  DEFAULT_SORT,
  type SortSpec,
  countMultiValuedField,
  filterByMultiValuedField,
  filterByStatus,
  sortRows,
} from '@/lib/library';
import { searchLibrary } from '@/lib/searchLibrary';
import type { Status } from '@/lib/status';
import { useAutoDensity } from '@/lib/useAutoDensity';
import { useSearch } from '@/state/search';

export const Route = createFileRoute('/')({
  component: LibraryHome,
});

function LibraryHome() {
  const seriesQuery = useSeriesList();
  const query = useSearch((s) => s.query);
  const density = useAutoDensity();
  const navigate = useNavigate();
  const onSelect = useCallback(
    (row: ListRow) => {
      if (!row.metadataRef) {
        return;
      }
      // Series detail addresses by metadata ref (provider:id) — same
      // identifier the list emits as `metadataRef`. Untracked rows
      // (no metadataRef) never reach this handler because the grid
      // only forwards onClick when the ref is present.
      void navigate({ to: '/series/$ref', params: { ref: row.metadataRef } });
    },
    [navigate],
  );

  const [active, setActive] = useState<Set<Status>>(new Set());
  const [activeSources, setActiveSources] = useState<Set<string>>(new Set());
  const [activeResolutions, setActiveResolutions] = useState<Set<string>>(new Set());
  const [sort, setSort] = useState<SortSpec>(DEFAULT_SORT);

  const trimmed = query.trim();
  // Local fuzzy search has zero network cost — fire on any non-empty
  // query. Single-character queries are useful for narrowing CJK
  // matches; the threshold + minMatchCharLength inside searchLibrary
  // keep noise out.
  const isSearching = trimmed.length > 0;

  const allRows = seriesQuery.data ?? [];

  const counts = useMemo(() => {
    const c: Partial<Record<Status, number>> = {};
    for (const row of allRows) {
      c[row.status] = (c[row.status] ?? 0) + 1;
    }
    return c;
  }, [allRows]);

  const sourceCounts = useMemo(
    () => countMultiValuedField(allRows, (r) => r.sources?.map(bucketSource)),
    [allRows],
  );
  const resolutionCounts = useMemo(
    () => countMultiValuedField(allRows, (r) => r.resolutions?.map(bucketResolution)),
    [allRows],
  );

  const rows = useMemo(() => {
    if (isSearching) {
      // Local fuzzy match against folded searchKey + display titles.
      // Fuse owns the result ordering for the search view; the
      // user's sort applies again when the query clears.
      return searchLibrary(allRows, trimmed);
    }
    let filtered = filterByStatus(allRows, active);
    filtered = filterByMultiValuedField(filtered, activeSources, (r) =>
      r.sources?.map(bucketSource),
    );
    filtered = filterByMultiValuedField(filtered, activeResolutions, (r) =>
      r.resolutions?.map(bucketResolution),
    );
    return sortRows(filtered, sort);
  }, [allRows, active, activeSources, activeResolutions, isSearching, trimmed, sort]);

  const toggleStatus = (status: Status) => {
    setActive((prev) => toggle(prev, status));
  };
  const toggleSource = (value: string) => {
    setActiveSources((prev) => toggle(prev, value));
  };
  const toggleResolution = (value: string) => {
    setActiveResolutions((prev) => toggle(prev, value));
  };
  const clearAllFilters = () => {
    setActive(new Set());
    setActiveSources(new Set());
    setActiveResolutions(new Set());
  };
  // Clear button is gated on at least one filter being active. Sort
  // is intentionally excluded — there's no "off" state for sort.
  const hasActiveFilters = active.size + activeSources.size + activeResolutions.size > 0;

  return (
    // Caps at 1920 px so super-wide displays don't sprawl. Below that
    // the grid fills the viewport. 24 px side padding ports from
    // scratch/webui-prototype/library-page-v2.jsx (`20px 24px 32px`).
    <div className="mx-auto max-w-[1920px] px-6 pt-5 pb-8">
      {!isSearching && (
        // Single row at every viewport. Mobile shrinks each control
        // to a 36 × 36 icon button so all four fit a 320 px viewport;
        // md+ expands to label + value pills.
        <header className="mb-4 flex flex-row flex-wrap items-center justify-between gap-2 md:gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <StatusFilterDropdown active={active} onToggle={toggleStatus} counts={counts} />
            <ValueFilterDropdown
              label="Source"
              icon="movie"
              values={SOURCE_ORDER}
              active={activeSources}
              onToggle={toggleSource}
              counts={sourceCounts}
              dotColors={SOURCE_DOTS}
            />
            <ValueFilterDropdown
              label="Resolution"
              icon="aspect_ratio"
              values={RESOLUTION_ORDER}
              active={activeResolutions}
              onToggle={toggleResolution}
              counts={resolutionCounts}
              dotColors={RESOLUTION_DOTS}
            />
            {hasActiveFilters && <ClearFiltersButton onClick={clearAllFilters} />}
          </div>
          <SortDropdown value={sort} onChange={setSort} />
        </header>
      )}

      {isSearching && (
        <header className="mb-4 flex items-baseline gap-3 text-sm">
          <SearchSummary query={trimmed} matchCount={rows.length} />
        </header>
      )}

      <LibraryBody
        seriesPending={seriesQuery.isPending}
        seriesError={seriesQuery.isError}
        librarySize={allRows.length}
        rows={rows}
        density={density}
        isSearching={isSearching}
        onSelect={onSelect}
      />
    </div>
  );
}

interface LibraryBodyProps {
  seriesPending: boolean;
  seriesError: boolean;
  librarySize: number;
  rows: readonly ListRow[];
  density: ReturnType<typeof useAutoDensity>;
  isSearching: boolean;
  onSelect: (row: ListRow) => void;
}

function LibraryBody({
  seriesPending,
  seriesError,
  librarySize,
  rows,
  density,
  isSearching,
  onSelect,
}: LibraryBodyProps) {
  if (seriesPending) {
    return <CenteredCard title="Loading library…" body="Walking the catalog." />;
  }
  if (seriesError) {
    return (
      <CenteredCard
        title="Couldn’t load the library"
        body="Refresh to try again, or check the server logs."
      />
    );
  }
  if (librarySize === 0) {
    return (
      <CenteredCard
        title="Library is empty"
        body="Add a series via the CLI (`kura add …`) and refresh — the grid will land here."
      />
    );
  }
  if (rows.length === 0 && !isSearching) {
    return (
      <CenteredCard
        title="No rows match the active filters"
        body="Open the filter menus and uncheck active items to widen the grid."
      />
    );
  }
  if (rows.length === 0 && isSearching) {
    // Header already conveys "no matches"; render nothing under it.
    return null;
  }
  return <VirtualPosterGrid rows={rows} density={density} onSelect={onSelect} />;
}

function toggle<T>(prev: ReadonlySet<T>, value: T): Set<T> {
  const next = new Set(prev);
  if (next.has(value)) {
    next.delete(value);
  } else {
    next.add(value);
  }
  return next;
}

// Hardcoded display order for the Source filter. Mirrors the
// quality-rank order surfaced elsewhere (BluRay best, TVRip worst,
// Unknown last). Anything the backend emits outside this set buckets
// into "Unknown" via bucketSource so an unfamiliar string never
// vanishes from the menu silently.
const SOURCE_ORDER: readonly string[] = [
  'BluRay',
  'Web-DL',
  'WebRip',
  'DVDRip',
  'TVRip',
  'Unknown',
];

const SOURCE_DOTS: Record<string, string> = {
  BluRay: 'bg-status-airing', // blue
  'Web-DL': 'bg-status-complete', // green
  WebRip: 'bg-status-incomplete', // yellow
  DVDRip: 'bg-orange-500',
  TVRip: 'bg-status-error', // red
  Unknown: 'bg-status-untracked', // gray
};

// Hardcoded display order for the Resolution filter. "Other" buckets
// every raw value outside the canonical four (1440p, 360p, raw WxH,
// etc.); ValueFilterDropdown's hide-when-zero behavior keeps it off
// the menu when the library has no exotic resolutions.
const RESOLUTION_ORDER: readonly string[] = ['4K', '1080p', '720p', '480p', 'Other'];

const RESOLUTION_DOTS: Record<string, string> = {
  '4K': 'bg-status-airing', // blue
  '1080p': 'bg-status-complete', // green
  '720p': 'bg-orange-500',
  '480p': 'bg-status-error', // red
  Other: 'bg-status-untracked', // gray
};

const SOURCE_BUCKET_SET = new Set(SOURCE_ORDER.filter((v) => v !== 'Unknown'));
function bucketSource(raw: string): string {
  return SOURCE_BUCKET_SET.has(raw) ? raw : 'Unknown';
}

const RESOLUTION_BUCKET_SET = new Set(RESOLUTION_ORDER.filter((v) => v !== 'Other'));
function bucketResolution(raw: string): string {
  return RESOLUTION_BUCKET_SET.has(raw) ? raw : 'Other';
}

function CenteredCard({ title, body }: { title: string; body: string }) {
  return (
    <div className="grid place-items-center py-16">
      <Card className="max-w-md p-6 text-center">
        <h2 className="text-sm font-semibold tracking-tight">{title}</h2>
        <p className="mt-1 text-sm text-muted">{body}</p>
      </Card>
    </div>
  );
}

interface SearchSummaryProps {
  query: string;
  matchCount: number;
}

function SearchSummary({ query, matchCount }: SearchSummaryProps) {
  if (matchCount === 0) {
    return <span className="text-muted">No matches for “{query}”.</span>;
  }
  const noun = matchCount === 1 ? 'match' : 'matches';
  return (
    <span className="text-muted">
      {matchCount} {noun} for “{query}”
    </span>
  );
}
