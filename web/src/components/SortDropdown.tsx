import { ChevronDown } from 'lucide-react';

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import type { SortDirection, SortKey, SortSpec } from '@/lib/library';

interface SortDropdownProps {
  value: SortSpec;
  onChange: (next: SortSpec) => void;
  className?: string;
}

const KEY_LABELS: Record<SortKey, string> = {
  title: 'Title',
  episodes: 'Episodes',
  status: 'Status',
};

const DIRECTION_GLYPH: Record<SortDirection, string> = {
  asc: '↑',
  desc: '↓',
};

const KEY_ORDER: readonly SortKey[] = ['title', 'episodes', 'status'];

const VALUE_SEP = '|';

function encode(spec: SortSpec): string {
  return `${spec.key}${VALUE_SEP}${spec.direction}`;
}

function decode(value: string): SortSpec {
  const [key, direction] = value.split(VALUE_SEP) as [SortKey, SortDirection];
  return { key, direction };
}

/**
 * Sort selector. Trigger shows the active key + direction inline so
 * the user sees the current state without opening the menu.
 *
 * Encodes the (key, direction) pair as a single radio value because
 * Radix RadioGroup only takes one string value at a time. The pipe
 * separator is internal — neither key nor direction contains it.
 */
export function SortDropdown({ value, onChange, className }: SortDropdownProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Sort"
          className={cn(
            // Square icon button on mobile (h-9 w-9, centered glyph) →
            // expanded pill on sm+ (auto width, gap-2, px-3). Sort has
            // no "all" state, so no active-dot indicator — the icon by
            // itself is the affordance.
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
          <span className="hidden sm:inline">
            {KEY_LABELS[value.key]} {DIRECTION_GLYPH[value.direction]}
          </span>
          <ChevronDown aria-hidden="true" className="hidden h-3.5 w-3.5 text-muted sm:inline" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Sort by</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={encode(value)} onValueChange={(v) => onChange(decode(v))}>
          {KEY_ORDER.map((key, i) => (
            <SortKeyGroup key={key} sortKey={key} showDivider={i > 0} />
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function SortKeyGroup({ sortKey, showDivider }: { sortKey: SortKey; showDivider: boolean }) {
  return (
    <>
      {showDivider && <DropdownMenuSeparator />}
      {(['asc', 'desc'] as const).map((direction) => (
        <DropdownMenuRadioItem
          key={`${sortKey}-${direction}`}
          value={encode({ key: sortKey, direction })}
        >
          <span>
            {KEY_LABELS[sortKey]} {DIRECTION_GLYPH[direction]}
          </span>
        </DropdownMenuRadioItem>
      ))}
    </>
  );
}
