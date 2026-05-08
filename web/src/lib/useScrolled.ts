import { useEffect, useState } from 'react';

/**
 * Tracks whether the document scroll has moved past `threshold` px.
 * Reads `window.scrollY` directly — kura pages ride window scroll
 * rather than nested overflow containers, so a single global listener
 * is enough.
 *
 * Used by the sticky `TopBar` (library home) and `BackBar` (series
 * detail) to swap chrome between resting + scrolled states (paper bg
 * vs translucent + shadow).
 */
export function useScrolled(threshold = 8): boolean {
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > threshold);
    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, [threshold]);
  return scrolled;
}
