import { useEffect, useState } from 'react';

export type DensityBreakpoint = 'xs' | 'sm' | 'md' | 'lg';

export interface Density {
  breakpoint: DensityBreakpoint;
  /** Minimum poster width (px). Drives the grid's auto-fit columns. */
  minPoster: number;
  /** Horizontal grid gap (px) between cards. */
  gap: number;
  /**
   * Vertical grid gap (px) between rows. Bigger than `gap` so titles
   * have breathing room before the next row's art lands.
   */
  rowGap: number;
  /** Tightens label sizes + gaps for the smaller poster cards. */
  dense: boolean;
}

interface DensityBucket {
  /** Inclusive lower bound, exclusive upper bound: [from, to). */
  from: number;
  to: number;
  density: Density;
}

const BUCKETS: readonly DensityBucket[] = [
  {
    from: 0,
    to: 480,
    density: { breakpoint: 'xs', minPoster: 96, gap: 12, rowGap: 24, dense: true },
  },
  {
    from: 480,
    to: 768,
    density: { breakpoint: 'sm', minPoster: 112, gap: 14, rowGap: 28, dense: true },
  },
  {
    from: 768,
    to: 1200,
    density: { breakpoint: 'md', minPoster: 140, gap: 16, rowGap: 32, dense: false },
  },
  {
    from: 1200,
    to: Number.POSITIVE_INFINITY,
    density: { breakpoint: 'lg', minPoster: 160, gap: 18, rowGap: 34, dense: false },
  },
];

const DEFAULT_DENSITY: Density = {
  breakpoint: 'md',
  minPoster: 140,
  gap: 16,
  rowGap: 32,
  dense: false,
};

/**
 * Pure function — returns the density for a given viewport width.
 * Exported for unit testing the boundary picks.
 */
export function pickDensity(width: number): Density {
  for (const b of BUCKETS) {
    if (width >= b.from && width < b.to) {
      return b.density;
    }
  }
  return DEFAULT_DENSITY;
}

/**
 * User-chosen grid density (Settings → Appearance). `comfortable` is
 * the viewport's own pick; `compact` asks for smaller posters.
 */
export type DensityPreference = 'comfortable' | 'compact';

/** One notch down the bucket ladder. `xs` is already the floor. */
const COMPACT_BREAKPOINT: Record<DensityBreakpoint, DensityBreakpoint> = {
  xs: 'xs',
  sm: 'xs',
  md: 'sm',
  lg: 'md',
};

/**
 * Pure function — folds the user's density preference into the
 * viewport-derived bucket. `compact` reuses the next-smaller bucket
 * wholesale (poster width, gaps, and the `dense` label treatment)
 * rather than inventing a parallel set of numbers, so the two
 * preferences stay visually consistent with each other at every
 * viewport. Exported for unit testing.
 */
export function applyDensityPreference(density: Density, preference: DensityPreference): Density {
  if (preference === 'comfortable') {
    return density;
  }
  const target = COMPACT_BREAKPOINT[density.breakpoint];
  return BUCKETS.find((b) => b.density.breakpoint === target)?.density ?? density;
}

/**
 * Subscribes to window resize and returns the current density bucket.
 * Falls back to the `md` bucket during SSR / before hydration so the
 * first frame doesn't pop layout once the real width arrives.
 */
export function useAutoDensity(): Density {
  const [density, setDensity] = useState<Density>(() => {
    if (typeof window === 'undefined') {
      return DEFAULT_DENSITY;
    }
    return pickDensity(window.innerWidth);
  });

  useEffect(() => {
    const onResize = () => {
      setDensity(pickDensity(window.innerWidth));
    };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  return density;
}
