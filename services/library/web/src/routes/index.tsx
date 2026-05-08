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

  const sourceCounts = useMemo(() => countMultiValuedField(allRows, (r) => r.sources), [allRows]);
  const resolutionCounts = useMemo(
    () => countMultiValuedField(allRows, (r) => r.resolutions),
    [allRows],
  );

  // Display order = count desc, alpha tiebreak. Server already
  // rank-sorts each row's array, but the across-row union is not
  // intrinsically ordered; falling back to "common values first" is
  // the kindest default until we have a strong domain ranking on the
  // client.
  const sourceValues = useMemo(() => sortByCountDesc(sourceCounts), [sourceCounts]);
  const resolutionValues = useMemo(() => sortByCountDesc(resolutionCounts), [resolutionCounts]);

  const rows = useMemo(() => {
    if (isSearching) {
      // Local fuzzy match against folded searchKey + display titles.
      // Fuse owns the result ordering for the search view; the
      // user's sort applies again when the query clears.
      return searchLibrary(allRows, trimmed);
    }
    let filtered = filterByStatus(allRows, active);
    filtered = filterByMultiValuedField(filtered, activeSources, (r) => r.sources);
    filtered = filterByMultiValuedField(filtered, activeResolutions, (r) => r.resolutions);
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
              values={sourceValues}
              active={activeSources}
              onToggle={toggleSource}
              counts={sourceCounts}
            />
            <ValueFilterDropdown
              label="Resolution"
              icon="aspect_ratio"
              values={resolutionValues}
              active={activeResolutions}
              onToggle={toggleResolution}
              counts={resolutionCounts}
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

function sortByCountDesc(counts: Record<string, number>): readonly string[] {
  const entries = Object.entries(counts).filter(([, n]) => n > 0);
  entries.sort((a, b) => {
    if (a[1] !== b[1]) {
      return b[1] - a[1];
    }
    return a[0].localeCompare(b[0]);
  });
  return entries.map(([k]) => k);
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
