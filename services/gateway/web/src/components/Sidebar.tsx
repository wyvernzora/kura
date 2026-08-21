import { Logo } from '@/components/Logo';
import { NAV, NavItem, SETTINGS_ITEM, useActiveNavId, useNavSelect } from '@/components/NavItem';
import { MaterialIcon } from '@/components/ui/material-icon';
import { cn } from '@/lib/cn';
import { useAppDefaultReset } from '@/lib/useAppDefaultReset';
import { useSidebar } from '@/state/sidebar';

/** Rail (icon-only) and expanded (labelled) widths, in px. */
const RAIL_WIDTH = 62;
const EXPANDED_WIDTH = 216;

/**
 * Desktop navigation rail. Sticky for the full viewport height beside
 * the scrolling page column; hidden below `md`, where the same nav
 * lives in `MobileDrawer`.
 *
 * Collapsed (the default) is a 62 px icon rail; expanded is 216 px
 * with labels. The width is an inline style rather than a class so
 * Tailwind isn't asked to generate a dynamic arbitrary value, and the
 * `transition-[width]` on the element animates between the two.
 *
 * The kura mark at the top and the Library item below it do the same
 * thing — reset to the app default view — because the library's
 * feature default is the app default.
 */
export function Sidebar({ className }: { className?: string }) {
  const rail = useSidebar((s) => s.collapsed);
  const toggle = useSidebar((s) => s.toggle);
  const active = useActiveNavId();
  const select = useNavSelect();
  const reset = useAppDefaultReset();

  return (
    <aside
      style={{ width: rail ? RAIL_WIDTH : EXPANDED_WIDTH }}
      className={cn(
        'sticky top-0 hidden h-dvh shrink-0 flex-col overflow-hidden md:flex',
        'border-line-soft border-r bg-paper px-3 py-[17px]',
        'transition-[width] duration-200 ease-out',
        rail && 'items-center',
        className,
      )}
    >
      <button
        type="button"
        onClick={reset}
        aria-label="kura — reset library view"
        className={cn(
          'mb-5 flex cursor-pointer items-center gap-2.5',
          rail ? 'justify-center' : 'px-1',
        )}
      >
        <Logo />
        {!rail && (
          <span className="whitespace-nowrap font-semibold text-[15px] text-ink tracking-[-0.2px]">
            kura
          </span>
        )}
      </button>

      <nav aria-label="Primary" className={cn('flex flex-col gap-1', rail && 'items-center')}>
        {NAV.map((item) => (
          <NavItem
            key={item.id}
            item={item}
            active={active === item.id}
            onSelect={select}
            rail={rail}
          />
        ))}
      </nav>

      {/* Bottom group: the collapse toggle sits directly above
          Settings so the two chrome-level controls read as one pair. */}
      <div className={cn('mt-auto flex flex-col gap-1', rail && 'items-center')}>
        <button
          type="button"
          onClick={toggle}
          aria-expanded={!rail}
          aria-label={rail ? 'Expand sidebar' : 'Collapse sidebar'}
          title={rail ? 'Expand sidebar' : undefined}
          className={cn(
            'flex cursor-pointer items-center rounded-md text-muted transition-colors duration-[120ms] hover:bg-overlay-soft hover:text-ink',
            rail ? 'h-[38px] w-[38px] justify-center' : 'h-9 w-full gap-2.5 px-2.5',
          )}
        >
          <MaterialIcon name={rail ? 'left_panel_open' : 'left_panel_close'} size={18} />
          {!rail && <span className="whitespace-nowrap font-medium text-sm">Collapse</span>}
        </button>
        <NavItem
          item={SETTINGS_ITEM}
          active={active === SETTINGS_ITEM.id}
          onSelect={select}
          rail={rail}
        />
      </div>
    </aside>
  );
}
