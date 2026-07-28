import { type MouseEvent, type RefObject, useCallback, useRef } from 'react';

interface PosterTiltOptions {
  /**
   * Maximum tilt angle along each axis, in degrees. Library home
   * uses 5 (toned-down "I'm clickable" hint); detail pages use 12-15
   * for a fuller AppleTV-style react.
   */
  tiltDeg?: number;
  /**
   * CSS perspective in px applied alongside the rotation. Smaller =
   * more pronounced 3D feel; 600 is the prototype sweet spot.
   */
  perspective?: number;
  /**
   * Pixels of upward translate combined into the same transform so a
   * separate hover-translate CSS class doesn't get clobbered by the
   * inline write. 0 disables the lift.
   */
  hoverLift?: number;
  /**
   * When false, the hook returns no-op handlers and never touches the
   * element. Use for static previews where cursor-tracked motion
   * would distract.
   */
  enabled?: boolean;
}

interface PosterTiltHandlers {
  onMouseEnter: () => void;
  onMouseMove: (e: MouseEvent<HTMLElement>) => void;
  onMouseLeave: () => void;
}

const NO_OP: PosterTiltHandlers = {
  onMouseEnter: () => {},
  onMouseMove: () => {},
  onMouseLeave: () => {},
};

/**
 * AppleTV-style cursor-tracked tilt hook. Caches the element rect on
 * mouseenter (cursor inside means rect is stable for the lifetime),
 * coalesces mousemove writes into a single rAF so transform updates
 * happen at most once per frame, and clears the inline transform on
 * mouseleave so the card snaps back.
 *
 * Avoids per-event getBoundingClientRect (forces layout) and keeps
 * scroll-frame pacing smooth on weak hardware.
 */
export function usePosterTilt(
  elementRef: RefObject<HTMLElement | null>,
  options: PosterTiltOptions = {},
): PosterTiltHandlers {
  const { tiltDeg = 5, perspective = 600, hoverLift = 0, enabled = true } = options;
  const cachedRectRef = useRef<DOMRect | null>(null);
  const lastPointerRef = useRef<{ x: number; y: number } | null>(null);
  const rafIdRef = useRef<number | null>(null);

  const onMouseEnter = useCallback(() => {
    cachedRectRef.current = elementRef.current?.getBoundingClientRect() ?? null;
  }, [elementRef]);

  const onMouseMove = useCallback(
    (e: MouseEvent<HTMLElement>) => {
      lastPointerRef.current = { x: e.clientX, y: e.clientY };
      if (rafIdRef.current !== null) {
        return;
      }
      rafIdRef.current = requestAnimationFrame(() => {
        rafIdRef.current = null;
        const pt = lastPointerRef.current;
        const el = elementRef.current;
        if (!pt || !el) {
          return;
        }
        let rect = cachedRectRef.current;
        if (!rect || rect.width === 0 || rect.height === 0) {
          rect = el.getBoundingClientRect();
          cachedRectRef.current = rect;
        }
        // Normalize cursor to [-1, +1] across the element's bounds.
        const px = ((pt.x - rect.left) / rect.width) * 2 - 1;
        const py = ((pt.y - rect.top) / rect.height) * 2 - 1;
        // Rotate toward cursor: top → tilt up (positive X axis pivots
        // the top edge away from the viewer); right → tilt right.
        const rotateX = -py * tiltDeg;
        const rotateY = px * tiltDeg;
        // Combine lift + tilt in one transform so the inline write
        // doesn't wipe a hover translate the CSS class would otherwise
        // apply.
        el.style.transform = `perspective(${perspective}px) translateY(${hoverLift}px) rotateX(${rotateX.toFixed(2)}deg) rotateY(${rotateY.toFixed(2)}deg)`;
      });
    },
    [elementRef, tiltDeg, perspective, hoverLift],
  );

  const onMouseLeave = useCallback(() => {
    if (rafIdRef.current !== null) {
      cancelAnimationFrame(rafIdRef.current);
      rafIdRef.current = null;
    }
    lastPointerRef.current = null;
    cachedRectRef.current = null;
    const el = elementRef.current;
    if (el) {
      el.style.transform = '';
    }
  }, [elementRef]);

  return enabled ? { onMouseEnter, onMouseMove, onMouseLeave } : NO_OP;
}
