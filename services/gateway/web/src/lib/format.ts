const MIB = 1024 * 1024;
const GIB = 1024 * MIB;
const KIB = 1024;

export function formatSize(bytes: number): string {
  if (bytes >= GIB) {
    return `${(bytes / GIB).toFixed(2)} GB`;
  }
  if (bytes >= MIB) {
    return `${Math.round(bytes / MIB)} MB`;
  }
  if (bytes >= KIB) {
    return `${Math.round(bytes / KIB)} KB`;
  }
  return `${bytes} B`;
}

/**
 * Server emits the storage marker `S01E0003`; episode rows display
 * the relaxed `S01E03` form to match the prototype. We chop the
 * leading two zeros off the episode pad when present and shorter
 * markers fall through unchanged.
 */
export function shortMarker(marker: string): string {
  return marker.replace(/^S(\d{2,})E(\d{4,})$/, (_m, season: string, episode: string) => {
    const ep = episode.replace(/^0+/, '') || '0';
    const padded = ep.length < 2 ? ep.padStart(2, '0') : ep;
    return `S${season}E${padded}`;
  });
}

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;

/**
 * Decimal byte units, matching the CLI's rendering — a 1 TB disk is
 * sold as 1 TB, and both trash sizes and release sizes are read
 * against disk capacity. Sub-GB values round to whole units (a media
 * file is never interesting at 0.1 MB resolution); GB and up keep one
 * decimal until they reach three digits.
 *
 * Distinct from `formatSize` above, which is binary (MiB / GiB) and
 * serves the series-detail episode rows.
 */
export function formatBytesDecimal(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1000 && unit < units.length - 1) {
    value /= 1000;
    unit += 1;
  }
  const rendered =
    unit >= 2
      ? value >= 100
        ? String(Math.round(value))
        : value.toFixed(1)
      : String(Math.round(value));
  return `${rendered} ${units[unit]}`;
}

/**
 * Coarse elapsed-time label: `m` under an hour, `h` under a day, `d`
 * under two months, `mo` beyond. Never renders `0m` — a just-created
 * entry reads as `1m`, since "0 minutes old" invites the reading that
 * the timestamp is missing.
 */
export function formatCompactAge(elapsedMs: number): string {
  if (elapsedMs < HOUR_MS) {
    return `${Math.max(1, Math.round(elapsedMs / 60_000))}m`;
  }
  if (elapsedMs < DAY_MS) {
    return `${Math.round(elapsedMs / HOUR_MS)}h`;
  }
  if (elapsedMs < 60 * DAY_MS) {
    return `${Math.round(elapsedMs / DAY_MS)}d`;
  }
  return `${Math.round(elapsedMs / (30 * DAY_MS))}mo`;
}

/**
 * Compact `YYYY-MM-DD HH:MM` stamp, rendered in UTC. UTC rather than
 * the viewer's zone so the string is stable across machines (the unit
 * tests and the Storybook fixtures both depend on that) and so a
 * server timestamp carrying an offset doesn't silently render in a
 * third zone. The relative age beside it carries the "how long ago"
 * reading, which is the actionable half.
 *
 * Returns an empty string for missing / unparseable input so callers
 * can render nothing without checking.
 */
export function formatStampUTC(iso: string | undefined): string {
  if (!iso) {
    return '';
  }
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) {
    return '';
  }
  const pad = (n: number) => String(n).padStart(2, '0');
  const date = `${at.getUTCFullYear()}-${pad(at.getUTCMonth() + 1)}-${pad(at.getUTCDate())}`;
  return `${date} ${pad(at.getUTCHours())}:${pad(at.getUTCMinutes())}`;
}

export function formatDateTime(iso: string | undefined): string | null {
  if (!iso) {
    return null;
  }
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) {
    return null;
  }
  return date.toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}
