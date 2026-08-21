import { describe, expect, it } from 'vitest';

import type { Candidate } from '@/api/types';
import { isAnimeCandidate } from './isAnimeCandidate';

function candidate(overrides: Partial<Candidate>): Candidate {
  return { ref: 'tvdb:1', preferredTitle: 'Show', ...overrides };
}

describe('isAnimeCandidate', () => {
  it('accepts an explicit Anime genre regardless of language', () => {
    expect(isAnimeCandidate(candidate({ genres: ['Anime'] }))).toBe(true);
    expect(
      isAnimeCandidate(candidate({ genres: ['Drama', 'Anime'], originalLanguage: 'en' })),
    ).toBe(true);
  });

  it('matches the Anime genre case-insensitively', () => {
    expect(isAnimeCandidate(candidate({ genres: ['anime'] }))).toBe(true);
  });

  it('accepts Animation only when the original language is Japanese', () => {
    expect(isAnimeCandidate(candidate({ genres: ['Animation'], originalLanguage: 'ja' }))).toBe(
      true,
    );
    expect(isAnimeCandidate(candidate({ genres: ['Animation'], originalLanguage: 'jpn' }))).toBe(
      true,
    );
    expect(isAnimeCandidate(candidate({ genres: ['Animation'], originalLanguage: 'en' }))).toBe(
      false,
    );
    expect(isAnimeCandidate(candidate({ genres: ['Animation'] }))).toBe(false);
  });

  it('rejects non-animated Japanese shows', () => {
    expect(isAnimeCandidate(candidate({ genres: ['Drama'], originalLanguage: 'ja' }))).toBe(false);
  });

  it('rejects candidates without genre data', () => {
    expect(isAnimeCandidate(candidate({}))).toBe(false);
    expect(isAnimeCandidate(candidate({ genres: [], originalLanguage: 'ja' }))).toBe(false);
  });
});
