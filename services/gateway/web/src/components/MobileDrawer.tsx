import { useEffect } from 'react';

import { Logo } from '@/components/Logo';
import {
  NAV,
  type NavId,
  NavItem,
  SETTINGS_ITEM,
  useActiveNavId,
  useNavSelect,
} from '@/components/NavItem';
import { GhostIconButton } from '@/components/ui/ghost-icon-btn';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { useAppDefaultReset } from '@/lib/useAppDefaultReset';

interface MobileDrawerProps {
  open: boolean;
  onClose: () => void;
  /**
   * Storybook seam: the drawer is `md:hidden`, so a story on a wide
   * canvas would render nothing. Passing `md:block` overrides that.
   * Production consumers leave this unset.
   */
  className?: string;
}

/**
 * Below `md` the sidebar is gone; this drawer carries the same nav.
 * Opened from the top bar's hamburger, closed by the overlay, the
 * close button, Escape, or by picking any destination.
 *
 * Items render in the sidebar's expanded style — a 250 px panel has
 * room for labels, and an icon rail would be a worse target on touch.
 */
export function MobileDrawer({ open, onClose, className }: MobileDrawerProps) {
  const active = useActiveNavId();
  const select = useNavSelect();
  const reset = useAppDefaultReset();

  useEffect(() => {
    if (!open) {
      return;
    }
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        onClose();
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, onClose]);

  if (!open) {
    return null;
  }

  function selectAndClose(id: NavId) {
    select(id);
    onClose();
  }

  return (
    <div className={cn('fixed inset-0 z-[60] md:hidden', className)}>
      {/* Scrim. Click-to-close is the expected drawer gesture; the
          Escape handler above covers the keyboard path, so the scrim
          itself stays out of the tab order and off the a11y tree. */}
      <button
        type="button"
        aria-hidden="true"
        tabIndex={-1}
        onClick={onClose}
        className="absolute inset-0 h-full w-full bg-black/30"
      />
      <div
        role="dialog"
        aria-label="Navigation"
        className="absolute inset-y-0 left-0 flex w-[250px] flex-col bg-paper px-3 py-[17px] shadow-pop"
      >
        <div className="mb-5 flex items-center gap-2.5 px-1">
          <button
            type="button"
            onClick={() => {
              reset();
              onClose();
            }}
            aria-label="kura — reset library view"
            className="flex cursor-pointer items-center gap-2.5"
          >
            <Logo />
            <span className="font-semibold text-[15px] text-ink tracking-[-0.2px]">kura</span>
          </button>
          <GhostIconButton size="md" aria-label="Close menu" onClick={onClose} className="ml-auto">
            <MaterialIcon name="close" size={18} />
          </GhostIconButton>
        </div>

        <nav aria-label="Primary" className="flex flex-col gap-1">
          {NAV.map((item) => (
            <NavItem
              key={item.id}
              item={item}
              active={active === item.id}
              onSelect={selectAndClose}
            />
          ))}
        </nav>

        <div className="mt-auto">
          <NavItem
            item={SETTINGS_ITEM}
            active={active === SETTINGS_ITEM.id}
            onSelect={selectAndClose}
          />
        </div>
      </div>
    </div>
  );
}
