import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useCallback } from 'react';

import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { useAppDefaultReset } from '@/lib/useAppDefaultReset';
import { useReleasesView } from '@/state/releasesView';
import { useSearch } from '@/state/search';
import { useTrashView } from '@/state/trashView';

export type NavId = 'library' | 'releases' | 'trash' | 'settings';

export interface NavEntry {
  id: NavId;
  label: string;
  /** Material Symbols glyph name. */
  icon: string;
}

/**
 * Primary navigation. Backups / jobs are not built yet, and a nav row
 * that lands on a placeholder is worse than no row, so they are absent
 * rather than disabled.
 */
export const NAV: readonly NavEntry[] = [
  { id: 'library', label: 'Library', icon: 'video_library' },
  { id: 'releases', label: 'Releases', icon: 'rss_feed' },
  { id: 'trash', label: 'Trash', icon: 'delete' },
];

/** Pinned to the bottom of the sidebar / drawer, outside `NAV`. */
export const SETTINGS_ITEM: NavEntry = { id: 'settings', label: 'Settings', icon: 'settings' };

/**
 * Which nav entry the current route belongs to. `/series/$ref` is part
 * of the library section, so anything that isn't Releases, Trash or
 * Settings highlights Library.
 */
export function useActiveNavId(): NavId {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  if (pathname === '/settings') {
    return 'settings';
  }
  if (pathname === '/releases') {
    return 'releases';
  }
  if (pathname === '/trash') {
    return 'trash';
  }
  return 'library';
}

/**
 * Click handler shared by the sidebar and the mobile drawer.
 *
 * - Library reruns the full app-default reset (filters, sort, search,
 *   scroll) even when it is already the active entry — the library's
 *   feature default is the app default, so a second click is the
 *   user's escape hatch out of a filtered / scrolled view.
 * - Trash follows the same rule for its own feature default: the age
 *   filter and sort go back to the full listing even on a re-click,
 *   so the destructive scope can never be narrowed by state the user
 *   has forgotten setting.
 * - Releases likewise returns to the attention set, so a re-click can
 *   never leave the operator staring at a queue narrowed by a filter
 *   they forgot they set.
 * - Settings is a plain navigation; there is nothing to reset.
 *
 * Every non-Library entry clears the search query: search is
 * page-scoped chrome and neither of those pages has a field to hold
 * it.
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
      if (id === 'releases') {
        useReleasesView.getState().clear();
        void navigate({ to: '/releases' });
        return;
      }
      if (id === 'trash') {
        useTrashView.getState().clear();
        void navigate({ to: '/trash' });
        return;
      }
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
