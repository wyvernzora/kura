import { type ButtonHTMLAttributes, forwardRef } from 'react';

import { cn } from '@/lib/cn';

interface IconBtnProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** Required label for icon-only buttons; assistive tech reads this. */
  'aria-label': string;
  /**
   * Pressed / on-state visual. Ink-filled square with paper glyph and
   * a flatter shadow — matches the prototype's "active" treatment for
   * the gear button when its menu is open.
   */
  active?: boolean;
  /**
   * Corner badge. Truthy boolean = empty pip; string/number = label
   * (e.g. unread count). Filled with incomplete-yellow per prototype.
   */
  badge?: boolean | string | number;
}

/**
 * Square icon button (36×36) used by all top-bar chrome — gear, theme
 * toggle, mobile search trigger. Always-elevated (shadow-card) so it
 * reads as a discrete control over paper. Active state flips the
 * surface to ink with a flatter shadow.
 */
export const IconBtn = forwardRef<HTMLButtonElement, IconBtnProps>(
  ({ className, active, badge, children, type, ...rest }, ref) => (
    <button
      ref={ref}
      type={type ?? 'button'}
      className={cn(
        'relative inline-flex h-9 w-9 items-center justify-center rounded-[8px]',
        'cursor-pointer transition-[box-shadow,background-color,color,transform] duration-[160ms] ease-out-soft',
        'disabled:pointer-events-none disabled:opacity-50',
        'focus:outline-none focus-visible:outline-none',
        'hover:-translate-y-px hover:shadow-card-hover',
        active
          ? 'bg-ink text-paper shadow-[0_1px_2px_rgba(31,29,26,0.18)]'
          : 'bg-surface text-ink shadow-card hover:bg-overlay-soft',
        className,
      )}
      {...rest}
    >
      {children}
      {badge != null && badge !== false && (
        <span
          aria-hidden="true"
          className={cn(
            'absolute -top-1 -right-1 inline-flex h-4 min-w-4 items-center justify-center',
            'rounded-full bg-status-incomplete px-1 font-bold text-[10px] text-ink',
            'shadow-[0_1px_2px_rgba(0,0,0,0.18)]',
          )}
        >
          {badge === true ? '' : badge}
        </span>
      )}
    </button>
  ),
);
IconBtn.displayName = 'IconBtn';
