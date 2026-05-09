import { RefreshCw } from 'lucide-react';
import { useRef, useState } from 'react';

import type { Show } from '@/api/types';
import { SeriesStatusCornerPill } from '@/components/series/SeriesStatusCornerPill';
import { cn } from '@/lib/cn';
import { formatRelativeAgo } from '@/lib/relativeTime';
import { primaryStatus, withAiring } from '@/lib/status';
import { usePosterTilt } from '@/lib/usePosterTilt';
import { PosterArt } from '@/poster/PosterArt';

/** Detail-page tilt is the prototype's full-range AppleTV reaction —
 *  larger angle than the library grid because there's only one card
 *  on screen and it carries the whole hero. */
const DETAIL_TILT_DEG = 14;

interface SeriesPosterCardProps {
  show: Show;
  /** Visual-only spinner state. P5 has no real scan flow; the button no-ops. */
  scanning?: boolean;
  onScanNow?: () => void;
  className?: string;
}

/**
 * Left column of the series detail layout. Stacks:
 *   - poster art (TVDB image preferred, deterministic SVG fallback)
 *     with status corner pill,
 *   - metadata ref (mono, muted),
 *   - title (h1, Inter 600),
 *   - "Scan now" button + last-scanned caption.
 *
 * The card sticks below `md:` so the right-column episode tables can
 * scroll independently without losing context of the series identity.
 */
export function SeriesPosterCard({ show, scanning, onScanNow, className }: SeriesPosterCardProps) {
  const status = primaryStatus(withAiring(show.status, !!show.isAiring));
  const title = show.preferredTitle || show.canonicalTitle || show.ref;
  const posterUrl = show.artwork?.poster?.thumbnailUrl ?? show.artwork?.poster?.url;
  return (
    <aside
      className={cn(
        'flex flex-col gap-3.5',
        // Mobile: cap the column to a sensible portrait size +
        // center, so a wide viewport doesn't blow up the artwork
        // and reveal compression in low-res posters. Desktop:
        // stretch into the 300 px grid track.
        'mx-auto w-full max-w-[240px] items-center text-center',
        'md:mx-0 md:max-w-none md:items-stretch md:text-left',
        // Stick below the 72 px TopBar (with a 24 px breath) so the
        // poster doesn't slide under the chrome on scroll.
        'md:sticky md:top-[96px] md:self-start',
        className,
      )}
    >
      <PosterFrame title={title} posterUrl={posterUrl} status={status} />
      <div className="font-mono text-[10px] tracking-[0.4px] text-muted">{show.metadataRef}</div>
      <h1 className="m-0 font-sans text-[26px] leading-tight font-semibold text-ink tracking-[-0.4px] [word-break:break-word]">
        {title}
      </h1>
      <button
        type="button"
        onClick={onScanNow}
        disabled={scanning}
        className={cn(
          'inline-flex h-[38px] items-center justify-center gap-2 self-stretch rounded-md',
          'border border-line-soft bg-surface px-4 text-sm font-medium text-ink shadow-card',
          'transition-[transform,box-shadow,background-color] duration-[160ms] ease-out',
          'hover:-translate-y-px hover:bg-overlay-soft hover:shadow-card-hover',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-overlay',
          'disabled:cursor-default disabled:opacity-60 disabled:hover:translate-y-0 disabled:hover:bg-surface disabled:hover:shadow-card',
        )}
      >
        <RefreshCw aria-hidden="true" className={cn('h-4 w-4', scanning && 'animate-spin')} />
        {scanning ? 'Scanning…' : 'Scan now'}
      </button>
      <div className="-mt-1 font-mono text-[10px] tracking-[0.4px] text-muted">
        {show.lastScanned ? `last scanned ${formatRelativeAgo(show.lastScanned)}` : 'never scanned'}
      </div>
    </aside>
  );
}

interface PosterFrameProps {
  title: string;
  posterUrl: string | undefined;
  status: ReturnType<typeof primaryStatus>;
}

function PosterFrame({ title, posterUrl, status }: PosterFrameProps) {
  const [imgError, setImgError] = useState(false);
  const [imgLoaded, setImgLoaded] = useState(false);
  const showImage = !!posterUrl && !imgError;
  const showSvg = !showImage || !imgLoaded;
  // Full-range cursor-tracked tilt — unlike the library grid's toned-
  // down ±5°, the hero poster gets the prototype's stronger ±14° so
  // the card visibly leans into the cursor. transform-origin centered
  // so the rotation pivots around the card's middle.
  const frameRef = useRef<HTMLDivElement>(null);
  const tiltHandlers = usePosterTilt(frameRef, {
    tiltDeg: DETAIL_TILT_DEG,
    hoverLift: -2,
  });
  return (
    <div
      ref={frameRef}
      onMouseEnter={tiltHandlers.onMouseEnter}
      onMouseMove={tiltHandlers.onMouseMove}
      onMouseLeave={tiltHandlers.onMouseLeave}
      className={cn(
        'relative aspect-[0.7] w-full overflow-hidden rounded-[12px] bg-surface',
        'shadow-poster transition-[box-shadow,transform] duration-[160ms] ease-out',
        'hover:shadow-poster-hover hover:will-change-transform [transform-origin:50%_50%]',
      )}
    >
      {showSvg && <PosterArt title={title} />}
      {showImage && (
        <img
          src={posterUrl}
          alt=""
          loading="lazy"
          decoding="async"
          onLoad={() => setImgLoaded(true)}
          onError={() => setImgError(true)}
          className="absolute inset-0 z-[1] h-full w-full object-cover"
        />
      )}
      <SeriesStatusCornerPill status={status} />
    </div>
  );
}
