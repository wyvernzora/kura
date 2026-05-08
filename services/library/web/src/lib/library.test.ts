import { describe, expect, it } from 'vitest';

import type { Candidate, ListRow } from '@/api/types';

import {
  countMultiValuedField,
  filterByMultiValuedField,
  filterByStatus,
  intersectWithCandidates,
  sortRows,
} from './library';

function row(overrides: Partial<ListRow> & { title: string }): ListRow {
  const base: ListRow = {
    status: 'complete',
    title: '',
    seasonsAvailable: 1,
    seasonCount: 1,
    episodesAvailable: 0,
    episodeCount: 0,
  };
  return { ...base, ...overrides };
}

const FRIEREN = row({
  title: '葬送のフリーレン',
  status: 'airing',
  episodesAvailable: 4,
  episodeCount: 28,
  metadataRef: 'tvdb:1',
});
const SPY = row({
  title: 'Spy x Family',
  status: 'complete',
  episodesAvailable: 25,
  episodeCount: 25,
  metadataRef: 'tvdb:2',
});
const ATTACK = row({
  title: '進撃の巨人',
  status: 'complete',
  episodesAvailable: 87,
  episodeCount: 87,
  metadataRef: 'tvdb:3',
});
const VINLAND = row({
  title: 'Vinland Saga',
  status: 'incomplete',
  episodesAvailable: 18,
  episodeCount: 24,
  metadataRef: 'tvdb:4',
});
const UNTRACKED = row({
  title: 'Mystery Folder',
  status: 'untracked',
  episodesAvailable: 0,
  episodeCount: 0,
});

const ALL = [FRIEREN, SPY, ATTACK, VINLAND, UNTRACKED];

describe('filterByStatus', () => {
  it('returns all rows when the filter set is empty', () => {
    expect(filterByStatus(ALL, new Set())).toEqual(ALL);
  });

  it('keeps only rows whose status is in the active set', () => {
    const result = filterByStatus(ALL, new Set(['complete']));
    expect(result).toEqual([SPY, ATTACK]);
  });

  it('handles multi-status filters', () => {
    const result = filterByStatus(ALL, new Set(['airing', 'incomplete']));
    expect(result).toEqual([FRIEREN, VINLAND]);
  });
});

describe('sortRows', () => {
  // Use Latin titles to keep ordering deterministic across locales —
  // CJK collation depends on reading order in some runtimes which is
  // not what the home grid is testing.
  const A = row({ title: 'Aria' });
  const B = row({ title: 'bocchi' }); // lowercase to verify case-insensitivity
  const C = row({ title: 'Cowboy' });

  it('sorts by title ascending case-insensitively', () => {
    const sorted = sortRows([C, A, B], { key: 'title', direction: 'asc' });
    expect(sorted.map((r) => r.title)).toEqual(['Aria', 'bocchi', 'Cowboy']);
  });

  it('reverses with desc', () => {
    const sorted = sortRows([C, A, B], { key: 'title', direction: 'desc' });
    expect(sorted.map((r) => r.title)).toEqual(['Cowboy', 'bocchi', 'Aria']);
  });

  it('sorts by episodes available ascending', () => {
    const sorted = sortRows([ATTACK, FRIEREN, SPY], { key: 'episodes', direction: 'asc' });
    expect(sorted.map((r) => r.title)).toEqual([FRIEREN.title, SPY.title, ATTACK.title]);
  });

  it('breaks ties with title', () => {
    const a = row({ title: 'B', episodesAvailable: 1, episodeCount: 1 });
    const b = row({ title: 'A', episodesAvailable: 1, episodeCount: 1 });
    const sorted = sortRows([a, b], { key: 'episodes', direction: 'asc' });
    expect(sorted.map((r) => r.title)).toEqual(['A', 'B']);
  });

  it('sorts by status with airing first', () => {
    const sorted = sortRows([SPY, FRIEREN, VINLAND, UNTRACKED], {
      key: 'status',
      direction: 'asc',
    });
    expect(sorted.map((r) => r.status)).toEqual(['airing', 'incomplete', 'complete', 'untracked']);
  });

  it('does not mutate the input', () => {
    const input = [SPY, FRIEREN, ATTACK];
    const snapshot = [...input];
    sortRows(input, { key: 'title', direction: 'asc' });
    expect(input).toEqual(snapshot);
  });
});

describe('filterByMultiValuedField', () => {
  const A = row({ title: 'A', sources: ['BluRay'], resolutions: ['1080p'] });
  const B = row({ title: 'B', sources: ['Web-DL', 'WebRip'], resolutions: ['1080p', '720p'] });
  const C = row({ title: 'C', sources: ['HDTV'], resolutions: ['720p'] });
  const D = row({ title: 'D' }); // no sources / resolutions
  const ROWS = [A, B, C, D];

  it('returns all rows when the active set is empty', () => {
    expect(filterByMultiValuedField(ROWS, new Set(), (r) => r.sources)).toEqual(ROWS);
  });

  it('keeps rows whose array intersects the active set', () => {
    const result = filterByMultiValuedField(ROWS, new Set(['Web-DL']), (r) => r.sources);
    expect(result).toEqual([B]);
  });

  it('treats multiple active values as union (OR)', () => {
    const result = filterByMultiValuedField(ROWS, new Set(['BluRay', 'HDTV']), (r) => r.sources);
    expect(result).toEqual([A, C]);
  });

  it('rows with missing field are excluded when the filter is active', () => {
    const result = filterByMultiValuedField(ROWS, new Set(['BluRay']), (r) => r.sources);
    expect(result).not.toContain(D);
  });

  it('honors a different getter', () => {
    const result = filterByMultiValuedField(ROWS, new Set(['720p']), (r) => r.resolutions);
    expect(result).toEqual([B, C]);
  });
});

describe('countMultiValuedField', () => {
  it('tallies distinct values across rows once per row', () => {
    const rows = [
      row({ title: 'A', sources: ['BluRay'] }),
      row({ title: 'B', sources: ['BluRay', 'Web-DL'] }),
      row({ title: 'C', sources: ['Web-DL', 'Web-DL'] }), // duplicate within row
      row({ title: 'D' }),
    ];
    const counts = countMultiValuedField(rows, (r) => r.sources);
    expect(counts).toEqual({ BluRay: 2, 'Web-DL': 2 });
  });

  it('returns an empty object when no rows have the field', () => {
    const rows = [row({ title: 'A' }), row({ title: 'B' })];
    expect(countMultiValuedField(rows, (r) => r.sources)).toEqual({});
  });
});

describe('intersectWithCandidates', () => {
  function candidate(ref: string): Candidate {
    return { ref, preferredTitle: ref };
  }

  it('returns empty array when no candidates', () => {
    expect(intersectWithCandidates(ALL, [])).toEqual([]);
  });

  it('keeps only rows whose metadataRef matches a candidate', () => {
    const result = intersectWithCandidates(ALL, [candidate('tvdb:2'), candidate('tvdb:1')]);
    expect(result.map((r) => r.metadataRef)).toEqual(['tvdb:2', 'tvdb:1']);
  });

  it('preserves candidate order, not row order', () => {
    const result = intersectWithCandidates(ALL, [candidate('tvdb:4'), candidate('tvdb:1')]);
    expect(result).toEqual([VINLAND, FRIEREN]);
  });

  it('excludes rows without a metadataRef', () => {
    const result = intersectWithCandidates(ALL, [candidate('tvdb:1')]);
    expect(result).toEqual([FRIEREN]);
    expect(result).not.toContain(UNTRACKED);
  });

  it('returns [] when no candidates overlap', () => {
    const result = intersectWithCandidates(ALL, [candidate('tvdb:999')]);
    expect(result).toEqual([]);
  });
});
