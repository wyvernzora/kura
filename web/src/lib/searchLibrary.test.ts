import { describe, expect, it } from 'vitest';

import type { ListRow } from '@/api/types';

import { searchLibrary } from './searchLibrary';

function row(overrides: Partial<ListRow> & { title: string }): ListRow {
  const base: ListRow = {
    status: 'complete',
    title: '',
    seasonsAvailable: 1,
    seasonCount: 1,
    episodesAvailable: 12,
    episodeCount: 12,
  };
  return { ...base, ...overrides };
}

const FRIEREN: ListRow = row({
  title: '葬送のフリーレン',
  canonicalTitle: 'Frieren: Beyond Journey’s End',
  searchKey: 'frieren\nbeyond\njourneys\nend\nsousou',
});
const OREIMO: ListRow = row({
  title: '俺の妹がこんなに可愛いわけがない',
  canonicalTitle: 'My Little Sister Can’t Be This Cute',
  // Server-folded user alias `oreimo` lands here.
  searchKey: 'oreimo\nlittle\nsister\ncute',
});
const COWBOY: ListRow = row({
  title: 'Cowboy Bebop',
  canonicalTitle: 'Cowboy Bebop',
});

const ROWS = [FRIEREN, OREIMO, COWBOY];

describe('searchLibrary', () => {
  it('returns all rows for empty query', () => {
    expect(searchLibrary(ROWS, '')).toEqual(ROWS);
    expect(searchLibrary(ROWS, '   ')).toEqual(ROWS);
  });

  it('matches via canonical title', () => {
    const result = searchLibrary(ROWS, 'cowboy');
    expect(result[0]).toBe(COWBOY);
  });

  it('matches via folded searchKey alias', () => {
    const result = searchLibrary(ROWS, 'oreimo');
    expect(result[0]).toBe(OREIMO);
  });

  it('matches romaji-aliased Japanese title via searchKey', () => {
    const result = searchLibrary(ROWS, 'frieren');
    expect(result[0]).toBe(FRIEREN);
  });

  it('matches CJK title substring via the title field', () => {
    // Fuse's score-based ranking varies for short CJK tokens, but the
    // matching row should appear somewhere in the result set.
    const result = searchLibrary(ROWS, 'フリーレン');
    expect(result).toContain(FRIEREN);
  });

  it('returns empty when no row matches', () => {
    expect(searchLibrary(ROWS, 'doraemon')).toEqual([]);
  });

  it('tolerates a 1-character typo at threshold 0.4', () => {
    // "freiren" → typo of "frieren"
    const result = searchLibrary(ROWS, 'freiren');
    expect(result[0]).toBe(FRIEREN);
  });
});
