import type { MediaShow } from '@/api/types';
import { formatSize } from '@/lib/format';

export interface ChangeRow {
  label: string;
  from?: string;
  to?: string;
  note?: string;
}

export function diffMedia(from: MediaShow, to: MediaShow): ChangeRow[] {
  const rows: ChangeRow[] = [];
  if (from.resolution !== to.resolution) {
    rows.push({ label: 'Resolution', from: from.resolution || '—', to: to.resolution || '—' });
  }
  if (from.codec !== to.codec) {
    rows.push({ label: 'Codec', from: from.codec || '—', to: to.codec || '—' });
  }
  if (from.size !== to.size) {
    rows.push({ label: 'File size', from: formatSize(from.size), to: formatSize(to.size) });
  }
  if (from.source !== to.source) {
    rows.push({ label: 'Source', from: from.source, to: to.source });
  }
  if (from.companions.length !== to.companions.length) {
    rows.push({
      label: 'Companions',
      from: String(from.companions.length),
      to: String(to.companions.length),
    });
  } else if (stableCompanions(from) !== stableCompanions(to)) {
    rows.push({ label: 'Companions', note: 'replaced or changed' });
  }
  if (stableAttrs(from.attrs) !== stableAttrs(to.attrs)) {
    rows.push({ label: 'Attributes', note: 'added, removed, or changed' });
  }
  return rows;
}

function stableCompanions(media: MediaShow): string {
  return JSON.stringify(
    media.companions
      .map((companion) =>
        JSON.stringify([
          companion.path,
          companion.role,
          companion.language,
          companion.label,
          companion.size,
          companion.mtime,
        ]),
      )
      .sort(),
  );
}

function stableAttrs(attrs: MediaShow['attrs']): string {
  return JSON.stringify(
    Object.entries(attrs ?? {}).sort(([left], [right]) => left.localeCompare(right)),
  );
}
