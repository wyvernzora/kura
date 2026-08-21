import { describe, expect, it } from 'vitest';

import type { TrashEmpty, TrashSeriesEntry } from '@/api/types.gen';

import {
  DEFAULT_TRASH_SORT,
  filterTrashByAge,
  formatTrashAge,
  formatTrashBytes,
  formatTrashStamp,
  nextTrashSort,
  olderThanParam,
  sortTrashGroups,
  trashEntryBytes,
  trashGroupBytes,
  trashOutcome,
} from './trash';

const NOW = Date.parse('2026-08-20T18:00:00Z');
const HOUR = 3_600_000;
const DAY = 24 * HOUR;

function entry(
  id: string,
  agedMs: number,
  sizeBytes: number,
  companions: number[] = [],
): TrashSeriesEntry['entries'][number] {
  return {
    id,
    episode: 'S01E0001',
    trashedAt: new Date(NOW - agedMs).toISOString(),
    mediaPath: `series:Season 1/${id}.mkv`,
    sizeBytes,
    companions: companions.map((sizeBytes, i) => ({ path: `series:${id}.${i}.srt`, sizeBytes })),
  };
}

function group(directory: string, entries: TrashSeriesEntry['entries']): TrashSeriesEntry {
  return {
    directory,
    ref: `tvdb:${directory}`,
    entries,
    bytes: entries.reduce((n, e) => n + e.sizeBytes, 0),
  };
}

describe('formatTrashBytes', () => {
  it('renders whole units below GB', () => {
    expect(formatTrashBytes(0)).toBe('0 B');
    expect(formatTrashBytes(999)).toBe('999 B');
    expect(formatTrashBytes(1000)).toBe('1 KB');
    expect(formatTrashBytes(812_000_000)).toBe('812 MB');
  });

  it('switches unit at the decimal 1000 boundary, not 1024', () => {
    expect(formatTrashBytes(1024)).toBe('1 KB');
    expect(formatTrashBytes(999_999_999)).toBe('1000 MB');
    expect(formatTrashBytes(1_000_000_000)).toBe('1.0 GB');
  });

  it('keeps one decimal for GB and up until three digits', () => {
    expect(formatTrashBytes(1_340_000_000)).toBe('1.3 GB');
    expect(formatTrashBytes(99_900_000_000)).toBe('99.9 GB');
    expect(formatTrashBytes(120_000_000_000)).toBe('120 GB');
    expect(formatTrashBytes(2_500_000_000_000)).toBe('2.5 TB');
  });
});

describe('formatTrashAge', () => {
  it('never renders a zero-minute age', () => {
    expect(formatTrashAge(0)).toBe('1m');
    expect(formatTrashAge(20_000)).toBe('1m');
  });

  it('crosses minute → hour → day → month buckets', () => {
    expect(formatTrashAge(59 * 60_000)).toBe('59m');
    expect(formatTrashAge(HOUR)).toBe('1h');
    expect(formatTrashAge(23 * HOUR)).toBe('23h');
    expect(formatTrashAge(DAY)).toBe('1d');
    expect(formatTrashAge(59 * DAY)).toBe('59d');
    expect(formatTrashAge(60 * DAY)).toBe('2mo');
  });
});

describe('formatTrashStamp', () => {
  it('renders YYYY-MM-DD HH:MM in UTC', () => {
    expect(formatTrashStamp('2026-08-20T18:04:09Z')).toBe('2026-08-20 18:04');
  });

  it('normalises an offset timestamp to UTC', () => {
    expect(formatTrashStamp('2026-08-20T18:04:09+09:00')).toBe('2026-08-20 09:04');
  });

  it('returns empty for missing or unparseable input', () => {
    expect(formatTrashStamp(undefined)).toBe('');
    expect(formatTrashStamp('not-a-date')).toBe('');
  });
});

describe('olderThanParam', () => {
  it('omits the parameter for any-age', () => {
    expect(olderThanParam(0)).toBeUndefined();
  });

  it('serialises every option as hours — Go time.ParseDuration has no day unit', () => {
    expect(olderThanParam(48)).toBe('48h');
    expect(olderThanParam(24 * 7)).toBe('168h');
    expect(olderThanParam(24 * 30)).toBe('720h');
    expect(olderThanParam(24 * 90)).toBe('2160h');
  });
});

describe('trash byte rollups', () => {
  it('counts companions toward an entry and its group', () => {
    const e = entry('a', 0, 1_000_000, [40_000, 2_000]);
    expect(trashEntryBytes(e)).toBe(1_042_000);
    expect(trashGroupBytes(group('G', [e, entry('b', 0, 500_000)]))).toBe(1_542_000);
  });
});

describe('filterTrashByAge', () => {
  const groups = [
    group('Fresh', [entry('f1', 2 * HOUR, 10)]),
    group('Mixed', [entry('m1', 2 * HOUR, 10), entry('m2', 10 * DAY, 20)]),
  ];

  it('returns everything when no filter is active', () => {
    expect(filterTrashByAge(groups, 0, NOW).map((g) => g.directory)).toEqual(['Fresh', 'Mixed']);
  });

  it('drops younger entries and the groups they emptied', () => {
    const filtered = filterTrashByAge(groups, 48, NOW);
    expect(filtered.map((g) => g.directory)).toEqual(['Mixed']);
    expect(filtered[0]?.entries.map((e) => e.id)).toEqual(['m2']);
  });

  it('is inclusive at the boundary, matching the server', () => {
    const exact = [group('Exact', [entry('e1', 48 * HOUR, 10)])];
    expect(filterTrashByAge(exact, 48, NOW)).toHaveLength(1);
    expect(filterTrashByAge(exact, 49, NOW)).toHaveLength(0);
  });
});

describe('sortTrashGroups', () => {
  const small = group('Bebop', [entry('s', 5 * DAY, 100)]);
  const large = group('Attack', [entry('l', 30 * DAY, 900)]);
  const recent = group('Frieren', [entry('r', 1 * HOUR, 400)]);

  it('sorts by size descending by default', () => {
    const sorted = sortTrashGroups([small, large, recent], DEFAULT_TRASH_SORT);
    expect(sorted.map((g) => g.directory)).toEqual(['Attack', 'Frieren', 'Bebop']);
  });

  it('flips with direction', () => {
    const sorted = sortTrashGroups([small, large, recent], { key: 'size', direction: 'asc' });
    expect(sorted.map((g) => g.directory)).toEqual(['Bebop', 'Frieren', 'Attack']);
  });

  it('sorts by most recently trashed under the trashed key', () => {
    const desc = sortTrashGroups([small, large, recent], { key: 'trashed', direction: 'desc' });
    expect(desc.map((g) => g.directory)).toEqual(['Frieren', 'Bebop', 'Attack']);
    const asc = sortTrashGroups([small, large, recent], { key: 'trashed', direction: 'asc' });
    expect(asc.map((g) => g.directory)).toEqual(['Attack', 'Bebop', 'Frieren']);
  });

  it('sorts by title case-insensitively', () => {
    const sorted = sortTrashGroups([small, large, recent], { key: 'title', direction: 'asc' });
    expect(sorted.map((g) => g.directory)).toEqual(['Attack', 'Bebop', 'Frieren']);
  });

  it('breaks size ties by title', () => {
    const b = group('Beta', [entry('b', DAY, 100)]);
    const a = group('Alpha', [entry('a', DAY, 100)]);
    expect(
      sortTrashGroups([b, a], { key: 'size', direction: 'asc' }).map((g) => g.directory),
    ).toEqual(['Alpha', 'Beta']);
  });

  it('does not mutate its input', () => {
    const input = [small, large];
    sortTrashGroups(input, DEFAULT_TRASH_SORT);
    expect(input.map((g) => g.directory)).toEqual(['Bebop', 'Attack']);
  });
});

describe('nextTrashSort', () => {
  it('flips direction when the active key is re-picked', () => {
    expect(nextTrashSort({ key: 'size', direction: 'desc' }, 'size')).toEqual({
      key: 'size',
      direction: 'asc',
    });
    expect(nextTrashSort({ key: 'size', direction: 'asc' }, 'size')).toEqual({
      key: 'size',
      direction: 'desc',
    });
  });

  it('starts a new key at its useful-first direction', () => {
    expect(nextTrashSort({ key: 'size', direction: 'asc' }, 'trashed')).toEqual({
      key: 'trashed',
      direction: 'desc',
    });
    expect(nextTrashSort({ key: 'size', direction: 'asc' }, 'title')).toEqual({
      key: 'title',
      direction: 'asc',
    });
  });
});

describe('trashOutcome', () => {
  const base: TrashEmpty = { series: [], totalEntries: 0, reclaimedBytes: 0 };

  it('reports none when the sweep attempted nothing', () => {
    expect(trashOutcome(base).kind).toBe('none');
    expect(trashOutcome({ ...base, attempts: 0 }).kind).toBe('none');
  });

  it('reports success when entries were removed', () => {
    const out = trashOutcome({
      ...base,
      attempts: 3,
      totalEntries: 3,
      reclaimedBytes: 4_200_000,
    });
    expect(out.kind).toBe('success');
    expect(out.entries).toBe(3);
    expect(out.bytes).toBe(4_200_000);
  });

  it('reports partial with per-series reasons when any series failed', () => {
    const out = trashOutcome({
      ...base,
      attempts: 4,
      totalEntries: 3,
      reclaimedBytes: 10,
      failures: [{ directory: 'Bebop', error: 'permission denied' }],
    });
    expect(out.kind).toBe('partial');
    expect(out.failures).toEqual([{ directory: 'Bebop', error: 'permission denied' }]);
  });

  it('prefers partial over none — a failed sweep did not "match nothing"', () => {
    const out = trashOutcome({
      ...base,
      attempts: 0,
      failures: [{ directory: 'Bebop', error: 'trash unreadable' }],
    });
    expect(out.kind).toBe('partial');
  });
});
