/**
 * Format an ISO-8601 / RFC-3339 timestamp as a coarse relative-ago
 * string for inline UI captions ("last scanned 4h ago"). Buckets
 * minute / hour / day; anything older than a day collapses to `Nd ago`
 * since the UI doesn't differentiate weeks vs days at this surface.
 *
 * Returns:
 *   - empty string for empty / invalid input (callers can render
 *     nothing without checking).
 *   - `just now` for < 1 minute.
 *   - `Nm ago` for < 60 minutes.
 *   - `Nh ago` for < 24 hours.
 *   - `Nd ago` otherwise.
 *
 * `now` is injectable for tests. Production callers omit it and pick
 * up the wall clock.
 */
export function formatRelativeAgo(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) {
    return '';
  }
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) {
    return '';
  }
  const elapsedMs = now.getTime() - t;
  if (elapsedMs < 60_000) {
    return 'just now';
  }
  const minutes = Math.floor(elapsedMs / 60_000);
  if (minutes < 60) {
    return `${minutes}m ago`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return `${hours}h ago`;
  }
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}
