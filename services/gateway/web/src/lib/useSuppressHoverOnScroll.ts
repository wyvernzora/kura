import { useEffect } from 'react';

const IDLE_MS = 100;

/**
 * While the page is actively scrolling, set `pointer-events: none`
 * on the document body. Standard scroll-perf trick: stops the
 * cursor from "hovering" cards as they slide past it, which would
 * otherwise fire mouseenter / mouseleave / :hover transitions /
 * getBoundingClientRect calls per row crossing — a large hidden
 * cost during fast scroll.
 *
 * Re-enabled after `IDLE_MS` of scroll silence so clicks resume
 * the moment the page settles. Scrollbar drag and the scroll input
 * itself are unaffected (scroll input lives on the viewport, not on
 * elements behind body).
 *
 * Mount once at the app shell — multiple instances would fight over
 * the inline style and stop cleanly only on the last unmount.
 */
export function useSuppressHoverOnScroll(): void {
  useEffect(() => {
    if (typeof document === 'undefined' || typeof window === 'undefined') {
      return;
    }
    let timeoutId: number | null = null;
    const onScroll = () => {
      document.body.style.pointerEvents = 'none';
      if (timeoutId !== null) {
        window.clearTimeout(timeoutId);
      }
      timeoutId = window.setTimeout(() => {
        document.body.style.pointerEvents = '';
        timeoutId = null;
      }, IDLE_MS);
    };
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      window.removeEventListener('scroll', onScroll);
      if (timeoutId !== null) {
        window.clearTimeout(timeoutId);
      }
      document.body.style.pointerEvents = '';
    };
  }, []);
}
