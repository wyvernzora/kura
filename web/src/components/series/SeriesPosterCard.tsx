import { useRef, useState } from 'react';

import type { ScanJobState } from '@/api/scanJob';
import type { Show } from '@/api/types';
import { ScanButton } from '@/components/series/ScanButton';
import { ScanDetailsModal, type ScanDetailsView } from '@/components/series/ScanDetailsModal';
import { SeriesStatusCornerPill } from '@/components/series/SeriesStatusCornerPill';
import { cn } from '@/lib/cn';
import { primaryStatus, withAiring } from '@/lib/status';
import { usePosterTilt } from '@/lib/usePosterTilt';
import { PosterArt } from '@/poster/PosterArt';

/** Detail-page tilt is the prototype's full-range AppleTV reaction —
 *  larger angle than the library grid because there's only one card
 *  on screen and it carries the whole hero. */
const DETAIL_TILT_DEG = 14;

interface SeriesPosterCardProps {
  show: Show;
  className?: string;
}

/**
 * Left column of the series detail layout. Stacks:
 *   - poster art (TVDB image preferred, deterministic SVG fallback)
 *     with status corner pill,
 *   - metadata ref (mono, muted),
 *   - title (h1, Inter 600),
 *   - "Scan now" button + progress + result caption.
 *
 * The card sticks below `md:` so the right-column episode tables can
 * scroll independently without losing context of the series identity.
 */
export function SeriesPosterCard({ show, className }: SeriesPosterCardProps) {
  const status = primaryStatus(withAiring(show.status, !!show.isAiring));
  const title = show.preferredTitle || show.canonicalTitle || show.ref;
  const posterUrl = show.artwork?.poster?.thumbnailUrl ?? show.artwork?.poster?.url;
  const [modalOpen, setModalOpen] = useState(false);
  const [view, setView] = useState<ScanDetailsView>(undefined);

  function handleShowDetails(scan: ScanJobState) {
    if (scan.phase === 'warning' && scan.skipped) {
      setView({ kind: 'warning', skipped: scan.skipped });
      setModalOpen(true);
      return;
    }
    if (scan.phase === 'error' && scan.error) {
      setView({
        kind: 'error',
        error: scan.error,
        progressFrozen: scan.progressFrozen,
      });
      setModalOpen(true);
    }
  }

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
      <ScanButton
        metadataRef={show.metadataRef}
        lastScanned={show.lastScanned}
        onShowDetails={handleShowDetails}
      />
      <ScanDetailsModal open={modalOpen} onOpenChange={setModalOpen} view={view} />
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
