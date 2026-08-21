import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useCallback, useMemo } from 'react';

import { useResolveSearch, useSeriesList } from '@/api/hooks';
import type { Candidate, ListRow } from '@/api/types';
import { ClearFiltersButton } from '@/components/ClearFiltersButton';
import { TvdbHintRow, TvdbSkeletonGrid } from '@/components/SearchScope';
import { SortDropdown } from '@/components/SortDropdown';
import { StatusFilterDropdown } from '@/components/StatusFilterDropdown';
import { Card } from '@/components/ui/card';
import { MaterialIcon } from '@/components/ui/material-icon';
import { Poster } from '@/components/ui/poster';
import { ValueFilterDropdown } from '@/components/ValueFilterDropdown';
import { VirtualPosterGrid } from '@/components/VirtualPosterGrid';
import {
  countMultiValuedField,
  filterByMultiValuedField,
  filterByStatus,
  sortRows,
} from '@/lib/library';
import { searchLibrary } from '@/lib/searchLibrary';
import type { Status } from '@/lib/status';
import { useAutoDensity } from '@/lib/useAutoDensity';
import { useLibraryFilters } from '@/state/library';
import { MIN_TVDB_QUERY_LENGTH, useSearch } from '@/state/search';

export const Route = createFileRoute('/')({
  component: LibraryHome,
});

function LibraryHome() {
  const seriesQuery = useSeriesList();
  const query = useSearch((s) => s.query);
  const clearSearch = useSearch((s) => s.clear);
  const scope = useSearch((s) => s.scope);
  const setScope = useSearch((s) => s.setScope);
  const density = useAutoDensity();
  const navigate = useNavigate();
  const onSelect = useCallback(
    (row: ListRow) => {
      if (!row.ref) {
        return;
      }
      // Picking a result is an explicit destination commit — the
      // search query has done its job, so clear it before navigating
      // so the destination page renders with an empty search field
      // (and the rewind-on-clear logic in TopBar doesn't bounce the
      // user back to where they started searching from).
      clearSearch();
      // Series detail addresses by metadata ref (provider:id) — same
      // identifier the list emits as `ref`. Untracked rows
      // (no ref) never reach this handler because the grid
      // only forwards onClick when the ref is present.
      void navigate({ to: '/series/$ref', params: { ref: row.ref } });
    },
    [navigate, clearSearch],
  );

  // Filters + sort live in a zustand store so they survive
  // navigation to /series/$ref and back. Local `useState` would
  // reset on every remount.
  const active = useLibraryFilters((s) => s.status);
  const activeSources = useLibraryFilters((s) => s.sources);
  const activeResolutions = useLibraryFilters((s) => s.resolutions);
  const sort = useLibraryFilters((s) => s.sort);
  const toggleStatus = useLibraryFilters((s) => s.toggleStatus);
  const toggleSource = useLibraryFilters((s) => s.toggleSource);
  const toggleResolution = useLibraryFilters((s) => s.toggleResolution);
  const setSort = useLibraryFilters((s) => s.setSort);
  const clearFilters = useLibraryFilters((s) => s.clear);

  const trimmed = query.trim();
  // Local fuzzy search has zero network cost — fire on any non-empty
  // query. Single-character queries are useful for narrowing CJK
  // matches; the threshold + minMatchCharLength inside searchLibrary
  // keep noise out.
  const isSearching = trimmed.length > 0;
  const tvdbEligible = trimmed.length >= MIN_TVDB_QUERY_LENGTH;

  const allRows = seriesQuery.data ?? [];

  // Metadata refs already in the library — drops provider matches the
  // user already owns from the TVDB scope.
  const libraryRefs = useMemo(
    () => new Set(allRows.map((r) => r.ref).filter((ref): ref is string => !!ref)),
    [allRows],
  );

  // Same query key as TopBar's prefetch — one request, shared cache.
  const resolve = useResolveSearch(tvdbEligible ? trimmed : '');
  const candidates = useMemo(
    () => (resolve.data?.candidates ?? []).filter((c) => !libraryRefs.has(c.ref)),
    [resolve.data, libraryRefs],
  );
  // Resolved-and-empty is a distinct state from unresolved: it hides
  // the hint row and swaps the no-matches CTA for a plain sentence.
  const tvdbEmpty = resolve.isSuccess && !resolve.isFetching && candidates.length === 0;
  const tvdbCount = resolve.isSuccess && !resolve.isFetching ? candidates.length : null;

  const onPickCandidate = useCallback(
    (c: Candidate) => {
      void navigate({ to: '/series/$ref', params: { ref: c.ref }, search: { preview: true } });
    },
    [navigate],
  );

  const counts = useMemo(() => {
    const c: Partial<Record<Status, number>> = {};
    for (const row of allRows) {
      c[row.status] = (c[row.status] ?? 0) + 1;
      if (row.isAiring) {
        c.airing = (c.airing ?? 0) + 1;
      }
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

  // Clear button is gated on at least one filter being active. Sort
  // is intentionally excluded; default sort is title asc.
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
            {hasActiveFilters && <ClearFiltersButton onClick={clearFilters} />}
          </div>
          <SortDropdown value={sort} onChange={setSort} />
        </header>
      )}

      {isSearching && scope === 'library' && (
        <header className="mb-4 flex items-baseline gap-3 text-sm">
          <SearchSummary query={trimmed} matchCount={rows.length} />
        </header>
      )}

      {isSearching && scope === 'library' && rows.length > 0 && tvdbEligible && (
        <div className="flex flex-col">
          <TvdbHintRow
            fetching={resolve.isFetching}
            count={tvdbCount}
            onGo={() => setScope('tvdb')}
          />
        </div>
      )}

      {isSearching && scope === 'library' && rows.length === 0 && (
        <NoLibraryMatches
          query={trimmed}
          tvdbEligible={tvdbEligible}
          tvdbEmpty={tvdbEmpty}
          tvdbCount={tvdbCount}
          onGo={() => setScope('tvdb')}
        />
      )}

      {isSearching && scope === 'tvdb' && (
        <TvdbResults
          query={trimmed}
          fetching={resolve.isFetching}
          empty={tvdbEmpty}
          candidates={candidates}
          onPick={onPickCandidate}
        />
      )}

      {scope === 'library' && (
        <LibraryBody
          seriesPending={seriesQuery.isPending}
          seriesError={seriesQuery.isError}
          librarySize={allRows.length}
          rows={rows}
          density={density}
          isSearching={isSearching}
          onSelect={onSelect}
        />
      )}
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

interface NoLibraryMatchesProps {
  query: string;
  tvdbEligible: boolean;
  tvdbEmpty: boolean;
  tvdbCount: number | null;
  onGo: () => void;
}

/**
 * Library scope with zero fuzzy matches: a plain sentence plus a
 * "See N on TVDB" jump when the provider has candidates. When TVDB
 * also resolved to zero, says so instead of offering a dead-end
 * button.
 */
function NoLibraryMatches({
  query,
  tvdbEligible,
  tvdbEmpty,
  tvdbCount,
  onGo,
}: NoLibraryMatchesProps) {
  return (
    <div className="flex flex-col items-start gap-3 py-8">
      <span className="text-sm text-muted">
        No library matches for “{query}”.
        {tvdbEligible && tvdbEmpty && ' TVDB has no matches either.'}
      </span>
      {tvdbEligible && !tvdbEmpty && (
        <button
          type="button"
          onClick={onGo}
          className="inline-flex h-9 items-center gap-2 rounded-md border border-line-soft bg-surface px-3 text-sm font-medium text-ink shadow-card transition-transform hover:-translate-y-px"
        >
          <MaterialIcon name="travel_explore" size={16} />
          {tvdbCount !== null ? `See ${tvdbCount} on TVDB` : 'Search TVDB'}
        </button>
      )}
    </div>
  );
}

interface TvdbResultsProps {
  query: string;
  fetching: boolean;
  empty: boolean;
  candidates: readonly Candidate[];
  onPick: (c: Candidate) => void;
}

/**
 * TVDB scope: a caption row (same h-8 + mb-4 box as TvdbHintRow so
 * the grid does not jump when switching scopes) above a poster grid
 * of provider candidates. Skeleton tiles while the resolve is in
 * flight; a plain sentence when it resolved to nothing.
 */
function TvdbResults({ query, fetching, empty, candidates, onPick }: TvdbResultsProps) {
  if (empty) {
    return <p className="py-8 text-sm text-muted">TVDB has no other matches for “{query}”.</p>;
  }
  return (
    <>
      <div className="mb-4 flex h-8 items-center gap-2 text-[13px] text-muted">
        {fetching && <MaterialIcon name="progress_activity" size={14} className="animate-spin" />}
        {fetching
          ? `Searching TVDB for “${query}”…`
          : 'Not in your library — click a result to preview and add.'}
      </div>
      {fetching ? (
        <TvdbSkeletonGrid />
      ) : (
        <div className="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(140px,1fr))]">
          {candidates.map((c) => (
            <Poster
              key={c.ref}
              title={c.year ? `${c.preferredTitle} (${c.year})` : c.preferredTitle}
              status="complete"
              posterUrl={c.posterUrl}
              posterThumbnailUrl={c.posterThumbnailUrl}
              onClick={() => onPick(c)}
            />
          ))}
        </div>
      )}
    </>
  );
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
