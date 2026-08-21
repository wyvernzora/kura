import { seriesRefOf } from '@/lib/releases';

/**
 * Best-effort season / episode extraction from a raw release title,
 * used to turn a hand-matched series into an episode-level ref.
 *
 * CEILING — this parser recognises three shapes and nothing else:
 * `SxxEyy`, a spaced ` - NN ` episode number, and `第N話` (with an
 * optional `第N期` season marker). Batches, absolute numbering across
 * seasons, arc titles, ordinal words and season names in prose all
 * fall through to `null`, and the dialog then submits a series-level
 * ref and says so. That is deliberate: the matcher owns real title
 * parsing, and a wrong guess here is silently written into an
 * operator match, which is certain by definition and therefore not
 * re-examined.
 */

export interface EpisodeMarker {
  season: number;
  episode: number;
}

/** `SxxEyy` in any case, with the common `.`/`_`/`-`/space separators. */
const SEASON_EPISODE = /\bs(\d{1,2})[\s._-]?e(\d{1,4})\b/i;

/** `第11話` — the episode counter, optionally spaced. */
const JP_EPISODE = /第\s*(\d{1,4})\s*話/;

/** `第2期` — the season counter that sometimes precedes it. */
const JP_SEASON = /第\s*(\d{1,2})\s*期/;

/**
 * A spaced ` - NN ` / ` - NN (` episode number. Global so the LAST
 * occurrence wins: arc-titled releases like
 * `Kimetsu no Yaiba - Hashira Geiko Hen - 09 [1080p]` put the episode
 * after the arc, never before it.
 */
const DASHED_EPISODE = /\s-\s(\d{1,4})(?=[\s([]|$)/g;

/**
 * Four-digit values in this band are years (`Monster (2004)`,
 * `Pack 2015-2019`), not episodes. Long-running series top out around
 * 1100, so the band costs nothing real.
 */
function looksLikeYear(value: number): boolean {
  return value >= 1900 && value <= 2099;
}

/**
 * Parses a release title into a season/episode marker, or `null` when
 * no recognised shape is present. A season-only title
 * (`Vinland.Saga.S02.1080p…`) yields `null`: the release is a batch,
 * not an episode.
 */
export function parseEpisodeMarker(title: string): EpisodeMarker | null {
  const sxe = SEASON_EPISODE.exec(title);
  if (sxe?.[1] && sxe[2]) {
    return { season: Number(sxe[1]), episode: Number(sxe[2]) };
  }

  const jp = JP_EPISODE.exec(title);
  if (jp?.[1]) {
    const season = JP_SEASON.exec(title);
    return { season: season?.[1] ? Number(season[1]) : 1, episode: Number(jp[1]) };
  }

  // Fresh lastIndex per call — the regex is module-scoped and global.
  DASHED_EPISODE.lastIndex = 0;
  let episode: number | null = null;
  for (const match of title.matchAll(DASHED_EPISODE)) {
    const value = Number(match[1]);
    if (!looksLikeYear(value)) {
      episode = value;
    }
  }
  // Assumed season 1: a bare episode number carries no season, and
  // single-cour releases are the overwhelming majority of this shape.
  return episode == null ? null : { season: 1, episode };
}

/** `{season: 1, episode: 29}` → `s01e29`. Wider numbers keep their digits. */
export function formatEpisodeMarker(marker: EpisodeMarker): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `s${pad(marker.season)}e${pad(marker.episode)}`;
}

/**
 * The ref submitted by a hand match: the resolved series ref plus an
 * `#sNNeNN` fragment when the release title parsed an episode. Any
 * fragment already on the series ref is replaced, so re-matching a
 * release that was matched before cannot stack fragments.
 */
export function composeReleaseRef(seriesRef: string, marker: EpisodeMarker | null): string {
  const series = seriesRefOf(seriesRef);
  return marker ? `${series}#${formatEpisodeMarker(marker)}` : series;
}
