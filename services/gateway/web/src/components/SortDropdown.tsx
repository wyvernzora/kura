import { ChevronDown } from 'lucide-react';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { nextSortForKey, type SortDirection, type SortKey, type SortSpec } from '@/lib/library';

interface SortDropdownProps {
  value: SortSpec;
  onChange: (next: SortSpec) => void;
  className?: string;
}

const KEY_LABELS: Record<SortKey, string> = {
  title: 'Title',
  episodes: 'Episode',
  status: 'Status',
  dateAdded: 'Date Added',
  lastAired: 'Last Aired',
};

const DIRECTION_GLYPH: Record<SortDirection, string> = {
  asc: '▲',
  desc: '▼',
};

const KEY_ORDER: readonly SortKey[] = ['title', 'episodes', 'status', 'dateAdded', 'lastAired'];

/**
 * Sort selector. Trigger shows the active key + direction inline so
 * the user sees the current state without opening the menu.
 *
 * Selecting a new key starts ascending. Selecting the active key
 * toggles between ascending and descending.
 */
export function SortDropdown({ value, onChange, className }: SortDropdownProps) {
  const label = `${KEY_LABELS[value.key]} ${DIRECTION_GLYPH[value.direction]}`;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Sort"
          className={cn(
            // Square icon button on mobile (h-9 w-9, centered glyph) →
            // expanded pill on sm+ (auto width, gap-2, px-3).
            'inline-flex h-9 w-9 items-center justify-center sm:w-auto sm:justify-start sm:gap-2 sm:px-3',
            'rounded-md border border-line-soft bg-surface text-sm text-ink shadow-card',
            'transition-[transform,box-shadow,background-color,color] duration-[160ms] ease-out',
            'hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
            className,
          )}
        >
          {/* Three layout tiers driven off the sm + lg breakpoints:
                <  sm  → icon only (square 36 × 36)
                sm-lg  → icon + value + chevron (no "Sort" label)
                ≥ lg   → icon + label + value + chevron (full pill)
          */}
          <MaterialIcon name="sort" />
          <span className="hidden text-muted lg:inline">Sort</span>
          <span className="hidden sm:inline">{label}</span>
          <ChevronDown aria-hidden="true" className="hidden h-3.5 w-3.5 text-muted sm:inline" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Sort by</DropdownMenuLabel>
        {KEY_ORDER.map((key) => (
          <SortKeyItem key={key} value={value} sortKey={key} onChange={onChange} />
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function SortKeyItem({
  value,
  sortKey,
  onChange,
}: {
  value: SortSpec;
  sortKey: SortKey;
  onChange: (next: SortSpec) => void;
}) {
  const selected = value.key === sortKey;
  return (
    <DropdownMenuItem className="pl-7" onSelect={() => onChange(nextSortForKey(value, sortKey))}>
      <span className="absolute left-2 inline-flex h-3.5 w-3.5 items-center justify-center">
        {selected ? DIRECTION_GLYPH[value.direction] : null}
      </span>
      <span>{KEY_LABELS[sortKey]}</span>
    </DropdownMenuItem>
  );
}
