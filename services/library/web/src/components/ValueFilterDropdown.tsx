import { ChevronDown } from 'lucide-react';

import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';

interface ValueFilterDropdownProps {
  /** Trigger label, e.g. "Source", "Resolution". */
  label: string;
  /**
   * Material Symbols glyph name for the mobile (sm:hidden) icon-only
   * trigger. See https://fonts.google.com/icons. The Outlined font is
   * loaded via `index.html`; consumers don't import anything.
   */
  icon: string;
  /** Display order for the menu items. Caller decides ranking. */
  values: readonly string[];
  /** Currently active values. Empty set = "Show all". */
  active: ReadonlySet<string>;
  /** Toggles a value in/out of the active set. */
  onToggle: (value: string) => void;
  /**
   * Required count map — values with `0` (or missing) are omitted from
   * the menu so users only see filters that would actually narrow the
   * grid.
   */
  counts: Record<string, number>;
  className?: string;
}

/**
 * Multi-select filter for multi-valued list fields (`sources`,
 * `resolutions`). Trigger and menu chrome match `SortDropdown` /
 * `StatusFilterDropdown` so the row of controls reads as a unit.
 *
 * Rows with `count === 0` (or missing from `counts`) are hidden — an
 * empty library shouldn't surface filters that match nothing.
 */
export function ValueFilterDropdown({
  label,
  icon,
  values,
  active,
  onToggle,
  counts,
  className,
}: ValueFilterDropdownProps) {
  const visible = values.filter((v) => (counts[v] ?? 0) > 0);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={`Filter by ${label.toLowerCase()}`}
          className={cn(
            // Three layout tiers driven off sm + lg:
            //   <  sm  → square icon button (36 × 36)
            //   sm-lg  → icon + summary + chevron (no label text)
            //   ≥ lg   → icon + label + summary + chevron (full pill)
            'relative inline-flex h-9 w-9 items-center justify-center sm:w-auto sm:justify-start sm:gap-2 sm:px-3',
            'rounded-md border border-line-soft bg-surface text-sm text-ink shadow-card',
            'transition-[transform,box-shadow,background-color,color] duration-[160ms] ease-out',
            'hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
            'disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:translate-y-0 disabled:hover:bg-surface disabled:hover:shadow-card',
            className,
          )}
          // Hide-when-empty: if no value would match anything, the
          // dropdown has nothing to offer. Disable rather than unmount
          // so the header layout stays stable across re-renders.
          disabled={visible.length === 0}
        >
          <MaterialIcon name={icon} />
          <span className="hidden text-muted lg:inline">{label}</span>
          <span className="hidden sm:inline">{summariseActive(active)}</span>
          <ChevronDown aria-hidden="true" className="hidden h-3.5 w-3.5 text-muted sm:inline" />
          {active.size > 0 && (
            <span
              aria-hidden="true"
              className="absolute top-1 right-1 h-1.5 w-1.5 rounded-full bg-status-airing sm:hidden"
            />
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[12rem]">
        <DropdownMenuLabel>Filter by {label.toLowerCase()}</DropdownMenuLabel>
        {visible.map((value) => (
          <DropdownMenuCheckboxItem
            key={value}
            checked={active.has(value)}
            onCheckedChange={() => onToggle(value)}
            // Keep the menu open across toggles — multi-select pattern.
            // Without preventDefault Radix closes the menu on every Item
            // activation.
            onSelect={(e) => e.preventDefault()}
          >
            <span className="flex w-full items-center gap-2">
              <span>{value}</span>
              <span className="ml-auto font-mono text-[11px] text-muted tabular-nums">
                {counts[value]}
              </span>
            </span>
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function summariseActive(active: ReadonlySet<string>): string {
  if (active.size === 0) {
    return 'All';
  }
  if (active.size === 1) {
    const [only] = active;
    return only ?? 'All';
  }
  return `${active.size} selected`;
}
