import { useEffect, useState } from 'react';

/**
 * Returns `value` delayed by `ms`. Each new value resets the timer,
 * so a fast-typing user sends only the final keystroke through.
 *
 * Pure debounce, not throttle: no leading or trailing-only modes,
 * no flushing on unmount. Safe to read in render.
 */
export function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return debounced;
}
