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
import { STATUS_LABELS, type Status } from '@/lib/status';

interface StatusFilterDropdownProps {
  /** Currently active filters. Empty set = "Show all". */
  active: ReadonlySet<Status>;
  /** Toggles the status in/out of the active set. */
  onToggle: (status: Status) => void;
  /**
   * Optional row count per status. Renders as a dim suffix after the
   * label so the user can see how many rows each filter would surface
   * without opening it.
   */
  counts?: Partial<Record<Status, number>>;
  className?: string;
}

const ORDER: readonly Status[] = ['airing', 'complete', 'incomplete', 'error', 'untracked'];

const DOT_BG: Record<Status, string> = {
  complete: 'bg-status-complete',
  incomplete: 'bg-status-incomplete',
  airing: 'bg-status-airing',
  untracked: 'bg-status-untracked',
  error: 'bg-status-error',
};

/**
 * Multi-select status filter. Trigger pill matches `SortDropdown`
 * (h-9 + line-soft border + lift on hover) so the two controls read
 * as a pair in the library header.
 *
 * Default "Show all" = empty active set. Selecting any status narrows
 * the grid; the trigger label summarises the active set without
 * opening the menu.
 *
 * Items keep the menu open after each toggle (`onSelect.preventDefault`)
 * so users can flip several statuses without re-opening the menu.
 */
export function StatusFilterDropdown({
  active,
  onToggle,
  counts,
  className,
}: StatusFilterDropdownProps) {
  // When counts are supplied, hide statuses with no rows. Without
  // counts (no library data yet) fall back to the full set so the menu
  // stays usable during initial load.
  const visible = counts ? ORDER.filter((s) => (counts[s] ?? 0) > 0) : ORDER;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label="Filter by status"
          className={cn(
            // Three layout tiers driven off sm + lg:
            //   <  sm  → square icon button (36 × 36)
            //   sm-lg  → icon + summary + chevron (no "Status" label)
            //   ≥ lg   → icon + label + summary + chevron (full pill)
            'relative inline-flex h-9 w-9 items-center justify-center sm:w-auto sm:justify-start sm:gap-2 sm:px-3',
            'rounded-md border border-line-soft bg-surface text-sm text-ink shadow-card',
            'transition-[transform,box-shadow,background-color,color] duration-[160ms] ease-out',
            'hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
            'disabled:cursor-not-allowed disabled:opacity-60 disabled:hover:translate-y-0 disabled:hover:bg-surface disabled:hover:shadow-card',
            className,
          )}
          disabled={visible.length === 0}
        >
          <MaterialIcon name="flag" />
          <span className="hidden text-muted lg:inline">Status</span>
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
      <DropdownMenuContent align="start" className="min-w-[14rem]">
        <DropdownMenuLabel>Filter by status</DropdownMenuLabel>
        {visible.map((status) => (
          <DropdownMenuCheckboxItem
            key={status}
            checked={active.has(status)}
            onCheckedChange={() => onToggle(status)}
            // Keep the menu open across toggles — multi-select pattern.
            // Without preventDefault Radix closes the menu on every Item
            // activation.
            onSelect={(e) => e.preventDefault()}
          >
            <span className="flex w-full items-center gap-2">
              <span
                aria-hidden="true"
                className={cn('h-2 w-2 shrink-0 rounded-full', DOT_BG[status])}
              />
              <span>{STATUS_LABELS[status]}</span>
              {typeof counts?.[status] === 'number' && (
                <span className="ml-auto font-mono text-[11px] text-muted tabular-nums">
                  {counts[status]}
                </span>
              )}
            </span>
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function summariseActive(active: ReadonlySet<Status>): string {
  if (active.size === 0) {
    return 'All';
  }
  if (active.size === 1) {
    const [only] = active;
    return only ? STATUS_LABELS[only] : 'All';
  }
  return `${active.size} selected`;
}
