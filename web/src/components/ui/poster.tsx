import { type KeyboardEvent, memo, useRef, useState } from 'react';

import { cn } from '@/lib/cn';
import type { StatusValue } from '@/lib/status';
import { usePosterTilt } from '@/lib/usePosterTilt';
import { PosterArt } from '@/poster/PosterArt';

/** Toned-down tilt angle for the library grid — strong enough to read as
 *  "this is interactive", subtle enough that a wall of cards doesn't
 *  feel chaotic. The series detail page uses a larger angle. */
const HOME_TILT_DEG = 5;

interface PosterProps {
  title: string;
  status: StatusValue;
  /**
   * Provider artwork URL (typically TVDB). Renders as an `<img>` over
   * the deterministic SVG; if loading fails the SVG remains visible
   * as the fallback.
   */
  posterUrl?: string;
  /**
   * Smaller variant of `posterUrl` for the grid; when present the UI
   * prefers it to keep the bytes cheap.
   */
  posterThumbnailUrl?: string;
  /** Episodes already present on disk. */
  available?: number;
  /** Total tracked episode count. */
  total?: number;
  /** Tightens the title font + gap on small viewports. */
  dense?: boolean;
  /** Suppress the title row (for use inside modals where the title lives separately). */
  hideTitle?: boolean;
  /**
   * Click / activate handler. When provided the cell becomes a
   * button: cursor pointer, role="button", Enter/Space keyboard
   * activation. Hover effects are independent and run regardless.
   */
  onClick?: () => void;
  /**
   * Disables hover effects (tilt + lift + shadow swap). Use for
   * static previews — modal posters, story compositions where
   * cursor-tracked motion would be a distraction.
   */
  noHover?: boolean;
  className?: string;
}

/**
 * Library-grid cell.
 *
 * Hover behaviour — toned-down AppleTV-style:
 *   - art card rotates toward the cursor (max ±5°) with CSS
 *     perspective; transform set inline on each mousemove for
 *     responsiveness, cleared on mouseleave so the card snaps back
 *   - shadow swaps from poster → poster-hover on :hover (CSS)
 *   - title row holds its baseline — only the art reacts
 *
 * Composition:
 *   wrapper             flex column, hover lift + click target.
 *   art card (inner)    aspect-[0.7], shadow-poster (3-stop) →
 *                       shadow-poster-hover on group hover.
 *                       container-type inline-size so cqi units in
 *                       UntrackedArt + ErrorMark scale with the cell.
 *   art layer           PosterArt (deterministic SVG) or UntrackedArt
 *                       (`?` placeholder).
 *   error overlay       ErrorMark on top of art for status=error.
 *   airing chip         AIRING pill top-left of art, only when status
 *                       includes "airing" and not in untracked/error.
 *   episode badge       Status-tinted pill bottom-right of art with
 *                       `available/total`; suppressed for untracked
 *                       and when episode totals are missing.
 *   title               Below the art, ≤2 lines, ink + Inter.
 */
function PosterImpl({
  title,
  status,
  posterUrl,
  posterThumbnailUrl,
  available,
  total,
  dense,
  hideTitle,
  onClick,
  noHover,
  className,
}: PosterProps) {
  const arr = Array.isArray(status) ? status : [status];
  const isUntracked = arr.includes('untracked');
  const isError = arr.includes('error');
  const isAiring = arr.includes('airing');
  const isIncomplete = arr.includes('incomplete');
  const showBadges = !isUntracked;
  const showEpisodeBadge = showBadges && available !== undefined && total !== undefined;

  const artRef = useRef<HTMLDivElement>(null);
  // Cursor-tracked tilt + 1 px lift, coalesced through rAF inside the
  // hook so transform writes happen at most once per frame.
  const tiltHandlers = usePosterTilt(artRef, {
    tiltDeg: HOME_TILT_DEG,
    hoverLift: -1,
    enabled: !noHover,
  });
  const [imgError, setImgError] = useState(false);
  const [imgLoaded, setImgLoaded] = useState(false);
  // Prefer the thumbnail (smaller bytes for grid cells) and fall back
  // to the full URL. Skip imagery entirely for untracked rows so the
  // `?` placeholder shows. For tracked rows the SVG paints underneath
  // until the photo loads, then unmounts so we don't pay double-paint
  // cost on every scroll-driven repaint.
  const imageSrc = posterThumbnailUrl ?? posterUrl;
  const showImage = !isUntracked && !!imageSrc && !imgError;
  const showSvgArt = !isUntracked && (!showImage || !imgLoaded);

  const handleKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (onClick && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault();
      onClick();
    }
  };

  return (
    <div
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      onClick={onClick}
      onKeyDown={onClick ? handleKey : undefined}
      onMouseEnter={tiltHandlers.onMouseEnter}
      onMouseMove={tiltHandlers.onMouseMove}
      onMouseLeave={tiltHandlers.onMouseLeave}
      className={cn(
        'group flex min-w-0 flex-col',
        // CSS layout containment: subtree layout changes can't ripple
        // back into the parent grid. Browser short-circuits layout
        // work on cells that aren't actively changing.
        //
        // NOT using `contain: paint` (which `contain: content` would
        // bring in) — that clips paint to the cell box, chopping off
        // the 3-stop poster shadow and the tilt rotation when the
        // card extends outside its wrapper bounds.
        '[contain:layout]',
        // Gap big enough that the 3-stop poster shadow doesn't bleed
        // onto the title row underneath.
        dense ? 'gap-2.5' : 'gap-3',
        onClick && 'cursor-pointer outline-none',
        className,
      )}
    >
      <div
        ref={artRef}
        className={cn(
          '@container relative aspect-[0.7] w-full overflow-hidden rounded-[10px] bg-surface',
          'shadow-poster',
          'transition-[box-shadow,transform] duration-[160ms] ease-out',
          !noHover && 'group-hover:-translate-y-px group-hover:shadow-poster-hover',
          // transform-origin centered so tilt pivots around the
          // card's middle. will-change is gated on :hover so only
          // the actively-tilting card gets promoted to its own
          // compositor layer — applying it at idle to every card on
          // screen burns GPU memory and tanks scroll FPS.
          !noHover && 'hover:will-change-transform [transform-origin:50%_50%]',
        )}
      >
        {isUntracked && <UntrackedArt />}
        {showSvgArt && <PosterArt title={title} />}
        {showImage && (
          <img
            src={imageSrc}
            alt=""
            loading="lazy"
            // decoding=async punts the JPEG decode off the main
            // thread so newly-visible posters during scroll don't
            // block paint while their bytes are unpacked.
            decoding="async"
            onLoad={() => setImgLoaded(true)}
            onError={() => setImgError(true)}
            className="absolute inset-0 z-[1] h-full w-full object-cover"
          />
        )}
        {isError && <ErrorMark />}
        {isAiring && !isUntracked && !isError && <AiringChip />}
        {showEpisodeBadge && (
          <EpisodeCountBadge airing={isAiring} incomplete={isIncomplete}>
            {available}/{total}
          </EpisodeCountBadge>
        )}
      </div>
      {!hideTitle && (
        <div
          title={title}
          className={cn(
            'overflow-hidden text-ink',
            'font-medium tracking-[-0.05px] leading-[1.3]',
            'line-clamp-2',
            dense ? 'text-[12px]' : 'text-[13px]',
          )}
        >
          {title}
        </div>
      )}
    </div>
  );
}

/**
 * Memoized export — TanStack Virtual calls flushSync on every scroll
 * event, which re-renders every visible cell. Posters' props don't
 * change during scroll (the rows array reference is stable across
 * scroll events), so React.memo's shallow compare short-circuits the
 * vast majority of those re-renders. Saves 20%+ of the scroll-frame
 * React budget on populated libraries.
 */
export const Poster = memo(PosterImpl);

function AiringChip() {
  return (
    <span
      aria-label="Airing"
      className={cn(
        'absolute top-2 left-2 z-[3] inline-flex h-[18px] items-center rounded-full px-[7px]',
        'bg-status-airing text-status-airing-fg',
        'font-mono text-[9px] font-bold tracking-[0.6px]',
      )}
      style={{ boxShadow: '0 1px 2px rgba(0,0,0,0.18), 0 0 0 1.5px rgba(255,255,255,0.6)' }}
    >
      AIRING
    </span>
  );
}

interface EpisodeCountBadgeProps {
  airing: boolean;
  incomplete: boolean;
  children: React.ReactNode;
}

function EpisodeCountBadge({ airing, incomplete, children }: EpisodeCountBadgeProps) {
  const style = incomplete
    ? 'bg-status-incomplete text-status-incomplete-fg'
    : airing
      ? 'bg-status-airing text-status-airing-fg'
      : 'bg-paper text-ink';
  return (
    <span
      className={cn(
        'absolute right-1.5 bottom-1.5 z-[3]',
        'rounded-[4px] px-1.5 py-0.5 font-mono text-[9px] font-bold tracking-[0.3px]',
        style,
      )}
      style={{ boxShadow: '0 1px 2px rgba(0,0,0,0.18), 0 0 0 1.5px rgba(255,255,255,0.45)' }}
    >
      {children}
    </span>
  );
}

function UntrackedArt() {
  return (
    <div
      aria-hidden="true"
      className="absolute inset-0 grid place-items-center font-mono leading-none font-bold"
      style={{
        background: '#ecebe5',
        color: '#9a9a95',
        fontSize: 'clamp(36px, 75cqi, 140px)',
      }}
    >
      ?
    </div>
  );
}

function ErrorMark() {
  return (
    <div
      aria-hidden="true"
      className="pointer-events-none absolute inset-0 z-[2] grid place-items-center"
      style={{ background: 'rgba(20,12,12,0.55)' }}
    >
      <svg
        viewBox="0 0 100 100"
        style={{
          width: '62cqi',
          height: '62cqi',
          maxWidth: 116,
          maxHeight: 116,
          filter: 'drop-shadow(0 1px 2px rgba(0,0,0,0.5))',
        }}
        aria-hidden="true"
      >
        <title>error</title>
        <path
          d="M50 12 L90 84 L10 84 Z"
          fill="none"
          stroke="#ff6b5a"
          strokeWidth="9"
          strokeLinejoin="round"
          strokeLinecap="round"
        />
        <line
          x1="50"
          y1="38"
          x2="50"
          y2="62"
          stroke="#ff6b5a"
          strokeWidth="9"
          strokeLinecap="round"
        />
        <circle cx="50" cy="74" r="5" fill="#ff6b5a" />
      </svg>
    </div>
  );
}
