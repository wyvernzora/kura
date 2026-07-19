import type { EpisodeStatus } from '@/api/types';

/**
 * Tooltip / aria label for each episode status. Consumed by
 * `StatusDot` and any caller that wants to surface the status
 * verbally (e.g. `aria-label`).
 */
export const EPISODE_STATUS_LABEL: Record<EpisodeStatus, string> = {
  present: 'Present',
  staged: 'Staged · awaiting reconcile',
  staged_replacement: 'Staged replacement · awaiting reconcile',
  missing: 'Missing · no file on disk',
  pending: 'Pending · not yet aired',
};

export const EPISODE_STATUS_BADGE: Record<
  EpisodeStatus,
  { label: string; icon: string; className: string }
> = {
  present: {
    label: 'Present',
    icon: 'check_circle',
    className: 'bg-status-complete/12 text-status-complete',
  },
  missing: {
    label: 'Missing',
    icon: 'report',
    className: 'bg-status-error/12 text-status-error',
  },
  pending: {
    label: 'Pending',
    icon: 'schedule',
    className: 'bg-status-airing/12 text-status-airing',
  },
  staged: {
    label: 'Staged',
    icon: 'move_to_inbox',
    className: 'bg-status-incomplete/16 text-status-staged-fg',
  },
  staged_replacement: {
    label: 'Staged replacement',
    icon: 'swap_horiz',
    className: 'bg-status-incomplete/16 text-status-staged-fg',
  },
};

/**
 * Status dot color, expressed as a Tailwind utility against the
 * existing kura status palette so light/dark theming flips for free.
 *
 * Staged states keep the base color of the on-disk reality so the
 * row reads at a glance: `staged` (no active record yet) shares the
 * missing color, `staged_replacement` (active record about to be
 * swapped) shares the present color. The pending-change signal is
 * carried by the amber outer glow on the dot itself (StatusDot's
 * `staged` prop), not by the base swatch.
 */
export const EPISODE_STATUS_DOT_BG: Record<EpisodeStatus, string> = {
  present: 'bg-status-complete',
  staged: 'bg-status-error',
  staged_replacement: 'bg-status-complete',
  missing: 'bg-status-error',
  pending: 'bg-status-airing',
};

/**
 * Whether the row should render dimmed. Pending episodes haven't
 * aired yet; we de-emphasise them so the user's eye lands on
 * present + missing first.
 */
export function isDimmedStatus(status: EpisodeStatus): boolean {
  return status === 'pending';
}

/**
 * Brief sub-text shown next to the air date in episode rows.
 * `null` means "no extra annotation"; the caller renders the air
 * date alone.
 */
export function episodeSubText(status: EpisodeStatus): string | null {
  switch (status) {
    case 'missing':
      return 'no file on disk';
    case 'pending':
      return 'not yet aired';
    default:
      return null;
  }
}

/**
 * Sub-optimal source values (matches prototype line 236). These render
 * as filled chips to draw attention; "premium" sources (BluRay, WebDL)
 * stay outlined. Server-side `Source.Display()` produces the strings
 * we're matching against — see `internal/domain/media/source.go`.
 */
const SUB_OPTIMAL_SOURCES = new Set(['WebRip', 'TV', 'TVRip', 'HDTV', 'DVDRip', 'Unknown']);

export function isSubOptimalSource(source: string): boolean {
  return SUB_OPTIMAL_SOURCES.has(source);
}

/**
 * Sub-optimal resolutions render filled. Matches prototype line 250
 * (anything that's not 4K / 2160p / 1080p is filled). Server-side
 * `Resolution.Display()` emits "1080p", "720p", "2160p" etc.
 */
const PREMIUM_RESOLUTIONS = new Set(['4K', '2160p', '1080p']);

export function isSubOptimalResolution(resolution: string): boolean {
  return !PREMIUM_RESOLUTIONS.has(resolution);
}

/**
 * Quality buckets map free-form source / resolution strings into the
 * existing kura status-color palette so chips can theme via tokens
 * (rather than the prototype's hardcoded hexes). Per-bucket choices
 * follow the prototype's intent: high-quality → blue (airing), main
 * library tier → green (complete), watchable-but-flagged → yellow
 * (incomplete), poor → red (error), unidentifiable → gray (untracked).
 */
export type QualityBucket = 'airing' | 'complete' | 'incomplete' | 'error' | 'untracked';

export function sourceBucket(source: string): QualityBucket {
  switch (source) {
    case 'BluRay':
      return 'airing';
    case 'Web-DL':
    case 'WebDL':
      return 'complete';
    case 'WebRip':
      return 'incomplete';
    case 'TV':
    case 'TVRip':
    case 'HDTV':
    case 'DVDRip':
      return 'error';
    default:
      return 'untracked';
  }
}

export function resolutionBucket(resolution: string): QualityBucket {
  switch (resolution) {
    case '4K':
    case '2160p':
      return 'airing';
    case '1080p':
      return 'complete';
    case '720p':
    case '480p':
      return 'incomplete';
    default:
      return 'error';
  }
}
