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
