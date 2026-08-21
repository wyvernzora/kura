import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useCallback } from 'react';

import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { useAppDefaultReset } from '@/lib/useAppDefaultReset';
import { useSearch } from '@/state/search';

export type NavId = 'library' | 'settings';

export interface NavEntry {
  id: NavId;
  label: string;
  /** Material Symbols glyph name. */
  icon: string;
}

/**
 * Primary navigation. One entry today — releases / backups / jobs /
 * trash are not built yet, and a nav row that lands on a placeholder
 * is worse than no row. The array shape is what it is so adding one
 * later is a one-line change.
 */
export const NAV: readonly NavEntry[] = [
  { id: 'library', label: 'Library', icon: 'video_library' },
];

/** Pinned to the bottom of the sidebar / drawer, outside `NAV`. */
export const SETTINGS_ITEM: NavEntry = { id: 'settings', label: 'Settings', icon: 'settings' };

/**
 * Which nav entry the current route belongs to. `/series/$ref` is part
 * of the library section, so anything that isn't Settings highlights
 * Library.
 */
export function useActiveNavId(): NavId {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  return pathname === '/settings' ? 'settings' : 'library';
}

/**
 * Click handler shared by the sidebar and the mobile drawer.
 *
 * - Library reruns the full app-default reset (filters, sort, search,
 *   scroll) even when it is already the active entry — the library's
 *   feature default is the app default, so a second click is the
 *   user's escape hatch out of a filtered / scrolled view.
 * - Settings is a plain navigation; there is nothing to reset. It
 *   still clears the query because search is page-scoped chrome and
 *   Settings has no search field to hold it.
 */
export function useNavSelect(): (id: NavId) => void {
  const navigate = useNavigate();
  const reset = useAppDefaultReset();

  return useCallback(
    (id: NavId) => {
      if (id === 'library') {
        reset();
        return;
      }
      useSearch.getState().clear();
      void navigate({ to: '/settings' });
    },
    [navigate, reset],
  );
}

interface NavItemProps {
  item: NavEntry;
  active: boolean;
  onSelect: (id: NavId) => void;
  /** Icon-rail mode: a 38 px square with the label as a tooltip. */
  rail?: boolean;
}

/**
 * One navigation row. A button rather than a link: Library's click is
 * a state reset that happens to navigate, so an `href` would promise
 * a plain page load it does not perform.
 */
export function NavItem({ item, active, onSelect, rail }: NavItemProps) {
  return (
    <button
      type="button"
      onClick={() => onSelect(item.id)}
      aria-current={active ? 'page' : undefined}
      title={rail ? item.label : undefined}
      className={cn(
        'flex cursor-pointer items-center rounded-md transition-colors duration-[120ms]',
        rail ? 'h-[38px] w-[38px] justify-center' : 'h-9 w-full gap-2.5 px-2.5',
        active
          ? 'bg-surface text-ink shadow-card'
          : 'text-muted hover:bg-overlay-soft hover:text-ink',
      )}
    >
      <MaterialIcon name={item.icon} size={18} />
      {!rail && <span className="whitespace-nowrap text-sm font-medium">{item.label}</span>}
    </button>
  );
}
