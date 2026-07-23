import { Search } from 'lucide-react';
import { forwardRef, type InputHTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

interface SearchFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  /**
   * When provided, a clear button appears whenever the input has a
   * value. Caller resets the value (controlled inputs only — fires
   * after the user clicks).
   */
  onClear?: () => void;
  /**
   * Hide the ⌘K hint pill (e.g. for mobile-takeover variants where the
   * shortcut isn't meaningful).
   */
  hideShortcutHint?: boolean;
}

/**
 * Prototype-spec search field — 38px tall, surface bg, shadow-card,
 * leading magnifier, optional ⌘K hint when empty, × clear button when
 * not.
 *
 * Width is the caller's responsibility — pass max-w-* / w-* via
 * `className`. The `kura-focusable` class wires the shared focus glow
 * so the wrapper picks up a blue halo when the inner input is
 * focused.
 */
export const SearchField = forwardRef<HTMLInputElement, SearchFieldProps>(
  ({ className, onClear, value, placeholder, hideShortcutHint, disabled, ...rest }, ref) => {
    const hasValue = typeof value === 'string' && value.length > 0;
    return (
      <div
        className={cn(
          'kura-focusable flex h-[38px] items-center gap-2.5 rounded-[8px]',
          'bg-surface px-3.5 shadow-card',
          'transition-shadow duration-[160ms] ease-out-soft',
          disabled && 'pointer-events-none opacity-60',
          className,
        )}
      >
        <Search aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-muted" />
        <input
          ref={ref}
          type="search"
          value={value}
          placeholder={placeholder ?? 'Search'}
          disabled={disabled}
          className={cn(
            'min-w-0 flex-1 border-0 bg-transparent p-0 text-sm text-ink',
            'placeholder:text-muted focus:outline-none',
          )}
          {...rest}
        />
        {hasValue && onClear ? (
          <button
            type="button"
            aria-label="Clear search"
            onClick={onClear}
            className={cn(
              'cursor-pointer border-0 bg-transparent p-0 font-mono text-sm leading-none text-muted',
              'transition-colors duration-quick ease-out-soft hover:text-ink',
            )}
          >
            ×
          </button>
        ) : !hideShortcutHint ? (
          <span
            aria-hidden="true"
            className="shrink-0 rounded-[4px] bg-line-soft px-1.5 py-0.5 font-mono text-[10px] text-muted"
          >
            ⌘K
          </span>
        ) : null}
      </div>
    );
  },
);
SearchField.displayName = 'SearchField';
