import { create } from 'zustand';
import { persist } from 'zustand/middleware';

interface SidebarStore {
  /** True → icon rail (62 px). False → labelled rail (216 px). */
  collapsed: boolean;
  toggle: () => void;
}

const SIDEBAR_STORAGE_KEY = 'kura.sidebar.collapsed';

/**
 * Desktop sidebar rail/expanded state. Persists to localStorage — the
 * chrome width is a durable workspace preference, not per-session
 * state, so a reload must not snap the sidebar back open (or shut).
 *
 * Defaults to collapsed: the icon rail is the resting shape, and the
 * poster grid gets the extra 154 px until the user asks for labels.
 */
export const useSidebar = create<SidebarStore>()(
  persist(
    (set) => ({
      collapsed: true,
      toggle: () => set((s) => ({ collapsed: !s.collapsed })),
    }),
    { name: SIDEBAR_STORAGE_KEY, version: 1 },
  ),
);
