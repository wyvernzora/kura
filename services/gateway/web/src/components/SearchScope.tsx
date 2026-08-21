import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import type { SearchScope } from '@/state/search';

/**
 * Inline spinner glyph. Material Symbols' progress_activity ring with
 * the standard spin animation; color inherits like any icon.
 */
function Spinner({ size = 13, className }: { size?: number; className?: string }) {
  return (
    <MaterialIcon name="progress_activity" size={size} className={cn('animate-spin', className)} />
  );
}

const SCOPE_ICONS: Record<SearchScope, string> = { library: 'hard_drive', tvdb: 'travel_explore' };

interface ScopeSegProps {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  label: string;
  icon: string;
  fetching?: boolean;
}

function ScopeSeg({ active, disabled, onClick, label, icon, fetching }: ScopeSegProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      aria-label={label}
      title={disabled ? 'Type at least 3 characters' : label}
      className={cn(
        'inline-flex h-full w-[38px] items-center justify-center rounded-[5px] transition-colors duration-[120ms]',
        active
          ? 'bg-ink text-paper'
          : disabled
            ? 'text-muted opacity-45'
            : 'text-muted hover:text-ink',
      )}
    >
      {fetching ? (
        <Spinner size={16} className={active ? 'text-paper/70' : 'text-muted'} />
      ) : (
        <MaterialIcon name={icon} size={17} />
      )}
    </button>
  );
}

interface SearchScopeControlProps {
  scope: SearchScope;
  onScope: (scope: SearchScope) => void;
  /** Provider resolve in flight — swaps the TVDB icon for a spinner. */
  tvdbFetching: boolean;
  /** Query below the TVDB floor — segment disabled with a tooltip. */
  tvdbDisabled: boolean;
}

/**
 * Icon-only segmented control that appears in the top bar right of
 * the search field while a search is active. Switches the results
 * view between local library matches and TVDB candidates not in the
 * library. Geometry mirrors SearchField (38px tall, same radius /
 * border / shadow) so the pair reads as one unit.
 */
export function SearchScopeControl({
  scope,
  onScope,
  tvdbFetching,
  tvdbDisabled,
}: SearchScopeControlProps) {
  return (
    <div
      role="tablist"
      aria-label="Search scope"
      className="inline-flex h-[38px] shrink-0 items-stretch gap-0.5 rounded-[8px] border border-line-soft bg-surface p-0.5 shadow-card"
    >
      <ScopeSeg
        active={scope === 'library'}
        onClick={() => onScope('library')}
        label="Library"
        icon={SCOPE_ICONS.library}
      />
      <ScopeSeg
        active={scope === 'tvdb'}
        disabled={tvdbDisabled}
        onClick={() => onScope('tvdb')}
        label="TVDB"
        icon={SCOPE_ICONS.tvdb}
        fetching={tvdbFetching}
      />
    </div>
  );
}

interface TvdbHintRowProps {
  /** Provider resolve still in flight. */
  fetching: boolean;
  /** Deduped candidate count; null while unresolved. */
  count: number | null;
  onGo: () => void;
}

/**
 * Slim ghost row above the library results: "N more on TVDB — not in
 * your library ›". Renders nothing once TVDB resolved to zero. Same
 * h-8 + mb-4 box as the TVDB scope's caption row so the poster grid
 * does not jump vertically when switching scopes.
 */
export function TvdbHintRow({ fetching, count, onGo }: TvdbHintRowProps) {
  if (!fetching && count === 0) {
    return null;
  }
  const label =
    fetching || count === null ? 'Checking TVDB…' : `${count} more on TVDB — not in your library`;
  return (
    <button
      type="button"
      onClick={onGo}
      className="-ml-2 mb-4 inline-flex h-8 items-center gap-1.5 self-start rounded-md px-2 text-[13px] font-medium text-muted transition-colors hover:bg-overlay-soft hover:text-ink"
    >
      {fetching ? <Spinner size={13} /> : <MaterialIcon name="travel_explore" size={15} />}
      <span>{label}</span>
      <MaterialIcon name="chevron_right" size={15} />
    </button>
  );
}

const SKELETON_TILES = ['s1', 's2', 's3', 's4', 's5'] as const;

/**
 * Placeholder grid shown in TVDB scope while the resolve is in
 * flight: five pulsing poster-shaped tiles plus a title line each.
 */
export function TvdbSkeletonGrid() {
  return (
    <div
      className="grid gap-4 [grid-template-columns:repeat(auto-fill,minmax(140px,1fr))]"
      aria-hidden="true"
    >
      {SKELETON_TILES.map((id) => (
        <div key={id} className="flex flex-col gap-3">
          <div className="aspect-[0.7] w-full animate-pulse rounded-[10px] bg-line-soft" />
          <div className="h-[13px] w-3/4 animate-pulse rounded bg-line-soft" />
        </div>
      ))}
    </div>
  );
}
