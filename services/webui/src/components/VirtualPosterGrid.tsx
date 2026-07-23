import { useWindowVirtualizer } from '@tanstack/react-virtual';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';

import type { ListRow } from '@/api/types';
import { Poster } from '@/components/ui/poster';
import { withAiring } from '@/lib/status';
import type { Density } from '@/lib/useAutoDensity';

interface VirtualPosterGridProps {
  rows: readonly ListRow[];
  density: Density;
  /**
   * Click handler for tracked posters. Untracked rows (no
   * `metadataRef`) skip this — they have no detail page to navigate
   * to and stay visually flat (no cursor pointer, no keyboard
   * affordance). Omit the prop entirely to keep every cell inert.
   */
  onSelect?: (row: ListRow) => void;
}

/**
 * Title block height under the art card. Picked off `density.dense`
 * because the dense buckets use a smaller font + tighter wrapper gap;
 * if the cell-height estimate overshoots actual, virtualizer leaves
 * an empty band below the title, and if it undershoots the next
 * row's poster eats into the title above it.
 *
 * Numbers must stay in sync with `Poster.tsx`'s title height
 * (h-[32px] dense, h-[34px] otherwise) and wrapper `gap-2.5` /
 * `gap-3` between art and title. Bump here if any of those move.
 */
function metaBlockPx(dense: boolean): number {
  return dense ? 10 + 32 : 12 + 34;
}
/** Poster art aspect = width / height = 7 / 10 — matches Poster card. */
const POSTER_ASPECT = 0.7;
/**
 * Render extra rows above and below the viewport so fast scrolls
 * don't flash. Each unit of overscan mounts `columnCount` Posters
 * (up to ~10 at lg breakpoint), so each row is real DOM work. 2
 * keeps a safety margin against fast scroll without bloating the
 * live mount count.
 */
const OVERSCAN_ROWS = 2;

/**
 * Virtualized grid of Posters. DOM stays bounded regardless of total
 * row count — only the rows in the viewport (+ overscan) are mounted.
 *
 * Layout:
 *   1. ResizeObserver tracks the container width and recomputes the
 *      column count from `density.minPoster` (the active breakpoint
 *      from useAutoDensity).
 *   2. Cell width follows from `(containerWidth - gaps) / columns`;
 *      cell height is `cellWidth / 0.7 + meta-row-height + gap` so
 *      every cell aligns to the Poster card's aspect ratio.
 *   3. useWindowVirtualizer uses the document scroll, with
 *      scrollMargin set to the grid's offsetTop so visible-row math
 *      compensates for whatever chrome (TopBar, header, filter row)
 *      sits above the grid.
 *
 * Accessibility note: the proper ARIA grid pattern (role=grid + arrow-
 * key cell navigation) needs a focus-management layer we haven't
 * built yet — rendering it here without the keyboard handlers would
 * fail axe audits anyway. P4 ships the visual virtualization with
 * plain DOM; the role layer lands alongside the keyboard nav in a
 * later iteration.
 */
export function VirtualPosterGrid({ rows, density, onSelect }: VirtualPosterGridProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [columnCount, setColumnCount] = useState(1);
  const [cellWidth, setCellWidth] = useState(density.minPoster);

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) {
      return;
    }
    const gap = density.gap;
    const compute = (width: number) => {
      const cols = Math.max(1, Math.floor((width + gap) / (density.minPoster + gap)));
      setColumnCount(cols);
      const w = cols > 0 ? (width - gap * (cols - 1)) / cols : density.minPoster;
      setCellWidth(w);
    };
    compute(el.clientWidth);
    const ro = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) {
        compute(entry.contentRect.width);
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, [density.minPoster, density.gap]);

  const posterHeight = cellWidth / POSTER_ASPECT;
  const rowHeight = posterHeight + metaBlockPx(density.dense) + density.rowGap;
  const rowCount = Math.ceil(rows.length / columnCount);

  const virtualizer = useWindowVirtualizer({
    count: rowCount,
    estimateSize: () => rowHeight,
    overscan: OVERSCAN_ROWS,
    scrollMargin: containerRef.current?.offsetTop ?? 0,
    // First param is the row element, second is the virtualizer
    // instance. Returning getBoundingClientRect().height tells the
    // library to use the actual rendered height instead of the
    // estimate — so sub-pixel poster aspect, line-clamp drift, and
    // breakpoint-driven font changes all stop showing up as gaps /
    // overlaps between rows.
    measureElement: (el) => el.getBoundingClientRect().height,
  });

  // Re-measure all rows whenever the cell width / breakpoint flips.
  // Without this the cache from the previous viewport sticks around
  // and rows render at the wrong vertical pitch until the user
  // scrolls past them. Biome flags the deps as "unused" because
  // virtualizer.measure() doesn't reference them in scope, but the
  // intent is exactly that — re-fire when the layout-critical
  // values change.
  // biome-ignore lint/correctness/useExhaustiveDependencies: see comment above
  useEffect(() => {
    virtualizer.measure();
  }, [virtualizer, cellWidth, density.dense, density.rowGap]);

  const totalSize = virtualizer.getTotalSize();
  const scrollMargin = virtualizer.options.scrollMargin ?? 0;

  return (
    <div ref={containerRef} style={{ position: 'relative', height: totalSize }}>
      {virtualizer.getVirtualItems().map((virtualRow) => {
        const startIndex = virtualRow.index * columnCount;
        const endIndex = Math.min(startIndex + columnCount, rows.length);
        const visible = rows.slice(startIndex, endIndex);
        return (
          <div
            key={virtualRow.key}
            ref={virtualizer.measureElement}
            data-index={virtualRow.index}
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              width: '100%',
              transform: `translateY(${virtualRow.start - scrollMargin}px)`,
              display: 'grid',
              gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
              // Column gap only — vertical pitch is owned by the
              // virtualizer's measured row height, so a CSS row-gap
              // here would double-up with the rowGap baked into the
              // estimate.
              columnGap: density.gap,
              // Each virtual-row container only has one row of cells,
              // but reserve `density.rowGap` of empty space at the
              // bottom via padding so consecutive rows breathe even
              // after dynamic measurement collapses the box to its
              // content.
              paddingBottom: density.rowGap,
            }}
          >
            {visible.map((row) => (
              <Poster
                key={row.metadataRef ?? row.title}
                title={row.title}
                status={withAiring(row.status, !!row.isAiring)}
                posterUrl={row.posterUrl}
                posterThumbnailUrl={row.posterThumbnailUrl}
                available={row.episodesAvailable}
                total={row.episodeCount}
                tags={row.tags}
                dense={density.dense}
                // Untracked rows have no metadata ref → no detail page →
                // stay flat. Tracked rows pick up cursor pointer +
                // role=button via Poster's onClick handling.
                onClick={onSelect && row.metadataRef ? () => onSelect(row) : undefined}
              />
            ))}
          </div>
        );
      })}
    </div>
  );
}
