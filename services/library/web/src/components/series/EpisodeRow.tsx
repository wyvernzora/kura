import { MoreHorizontal } from 'lucide-react';
import type { ReactNode } from 'react';

import type { EpisodeShow, MediaShow } from '@/api/types';
import { CompanionsBadge } from '@/components/series/CompanionsBadge';
import { ChipSlot, ResolutionChip, SourceChip } from '@/components/series/QualityChip';
import { StatusDot } from '@/components/series/StatusDot';
import { cn } from '@/lib/cn';
import { episodeSubText, isDimmedStatus } from '@/lib/episodeStatus';

interface EpisodeRowProps {
  episode: EpisodeShow;
  className?: string;
}

const MIB = 1024 * 1024;
const GIB = 1024 * MIB;

function formatSize(bytes: number): string {
  if (bytes >= GIB) {
    return `${(bytes / GIB).toFixed(2)} GB`;
  }
  if (bytes >= MIB) {
    return `${Math.round(bytes / MIB)} MB`;
  }
  return `${bytes} B`;
}

/**
 * One row in a season's episode table. Two layouts driven off the sm
 * breakpoint:
 *
 *   sm+ (desktop):  single-row 6-col grid
 *                   [dot] [marker] [title + sub] [src] [res] [⋯]
 *
 *   <sm (mobile):   two-row stack — meta first, then title with
 *                   compact chips inline:
 *                   [dot] marker · airdate · codec · size       [⋯]
 *                         title (+N)                            [src][res]
 *
 * The desktop chips are 64 × 22 so they line up across rows; on
 * mobile they shrink to content-width (18 high, ~36-50 wide) so they
 * fit alongside the title without squeezing it down to an ellipsis.
 *
 * Pending rows render dimmed; missing / pending rows render chip
 * placeholders on desktop so columns line up regardless of which
 * cells have media on disk.
 */
export function EpisodeRow({ episode, className }: EpisodeRowProps) {
  const { status, episode: marker, preferredTitle, canonicalTitle, aired, active } = episode;
  const title = preferredTitle || canonicalTitle || marker;
  const dim = isDimmedStatus(status);
  const hasMedia = !!active;
  const hasStaged = !!episode.staged;
  const sub = episodeSubText(status);
  const companions = active?.companions?.length ?? 0;
  const subText = renderSubText(aired, active, sub);
  return (
    <div
      className={cn(
        'border-line-soft border-b transition-colors duration-[120ms] ease-out last:border-b-0',
        'hover:bg-overlay-soft',
        dim && 'opacity-60',
        className,
      )}
    >
      <DesktopRow
        marker={marker}
        title={title}
        subText={subText}
        companions={companions}
        status={status}
        hasMedia={hasMedia}
        hasStaged={hasStaged}
        active={active}
      />
      <MobileRow
        marker={marker}
        title={title}
        subText={subText}
        companions={companions}
        status={status}
        hasMedia={hasMedia}
        hasStaged={hasStaged}
        active={active}
      />
    </div>
  );
}

interface RowVariantProps {
  marker: string;
  title: string;
  subText: ReactNode;
  companions: number;
  status: EpisodeShow['status'];
  hasMedia: boolean;
  hasStaged: boolean;
  active: MediaShow | undefined;
}

function DesktopRow({
  marker,
  title,
  subText,
  companions,
  status,
  hasMedia,
  hasStaged,
  active,
}: RowVariantProps) {
  return (
    <div
      className="hidden items-center gap-3 px-[18px] py-3 sm:grid"
      style={{ gridTemplateColumns: '14px 56px 1fr auto auto auto' }}
    >
      <StatusDot status={status} staged={hasStaged} />
      <div className="text-right font-mono text-[11px] text-muted">{shortMarker(marker)}</div>
      <div className="min-w-0">
        <div className="flex items-center text-[13px] leading-tight font-medium text-ink tracking-[-0.05px]">
          <span className="truncate">{title}</span>
          <CompanionsBadge count={companions} />
        </div>
        <div className="mt-[3px] font-mono text-[10px] tracking-[0.3px] text-muted">{subText}</div>
      </div>
      {hasMedia && active ? <SourceChip source={active.source} /> : <ChipSlot />}
      {hasMedia && active?.resolution ? (
        <ResolutionChip resolution={active.resolution} />
      ) : (
        <ChipSlot />
      )}
      <ActionsButton />
    </div>
  );
}

function MobileRow({
  marker,
  title,
  subText,
  companions,
  status,
  hasMedia,
  hasStaged,
  active,
}: RowVariantProps) {
  return (
    <div className="flex flex-col gap-1 px-3 py-3 sm:hidden">
      {/* Meta line: dot, marker · airdate · codec · size, ⋯ */}
      <div className="flex items-center gap-2">
        <StatusDot status={status} staged={hasStaged} />
        <div className="min-w-0 flex-1 truncate font-mono text-[10px] tracking-[0.3px] text-muted">
          {shortMarker(marker)} · {subText}
        </div>
        <ActionsButton className="shrink-0" />
      </div>
      {/* Title line: indented to align with the meta text above; chips
          ride on the right at compact size so they fit without
          competing with the title. */}
      <div className="flex items-center gap-2 pl-[18px]">
        <div className="flex min-w-0 flex-1 items-center text-[13px] leading-tight font-medium text-ink tracking-[-0.05px]">
          <span className="truncate">{title}</span>
          <CompanionsBadge count={companions} />
        </div>
        {hasMedia && active ? <SourceChip size="compact" source={active.source} /> : null}
        {hasMedia && active?.resolution ? (
          <ResolutionChip size="compact" resolution={active.resolution} />
        ) : null}
      </div>
    </div>
  );
}

function renderSubText(
  aired: string | undefined,
  active: MediaShow | undefined,
  subAnnotation: string | null,
): ReactNode {
  const parts: string[] = [];
  parts.push(aired || '—');
  if (active?.codec) {
    parts.push(active.codec);
  }
  if (active && active.size > 0) {
    parts.push(formatSize(active.size));
  }
  if (subAnnotation) {
    parts.push(subAnnotation);
  }
  return parts.join(' · ');
}

function ActionsButton({ className }: { className?: string }) {
  return (
    <button
      type="button"
      aria-label="Episode actions"
      // P5 keeps the affordance visible but inert. Episode-level
      // mutations (rescan, identify, mark missing) land in a later
      // round when the action menu wires up.
      disabled
      className={cn(
        'inline-flex h-7 w-7 items-center justify-center rounded-md text-muted disabled:cursor-default disabled:opacity-60',
        className,
      )}
    >
      <MoreHorizontal aria-hidden="true" className="h-4 w-4" />
    </button>
  );
}

/**
 * Server emits the storage marker `S01E0003`; episode rows display
 * the relaxed `S01E03` form to match the prototype. We chop the
 * leading two zeros off the episode pad when present and shorter
 * markers fall through unchanged.
 */
function shortMarker(marker: string): string {
  return marker.replace(/^S(\d{2,})E(\d{4,})$/, (_m, season: string, episode: string) => {
    const ep = episode.replace(/^0+/, '') || '0';
    const padded = ep.length < 2 ? ep.padStart(2, '0') : ep;
    return `S${season}E${padded}`;
  });
}
