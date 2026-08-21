import { describe, expect, it } from 'vitest';

import type { ListRow } from '@/api/types';

import {
  confidenceColor,
  confidencePercent,
  isLowConfidence,
  LOW_CONFIDENCE,
  releaseActions,
  seriesRefOf,
  seriesTitleForRef,
  seriesTitleIndex,
} from './releases';

const RED = 'var(--color-status-error)';
const ORANGE = '#d97b32';
const YELLOW = 'var(--color-status-incomplete)';
const GREEN = 'var(--color-status-complete)';

describe('confidenceColor', () => {
  it('walks the ladder red → orange → yellow → green', () => {
    expect(confidenceColor(0.0)).toBe(RED);
    expect(confidenceColor(0.51)).toBe(RED);
    expect(confidenceColor(0.52)).toBe(ORANGE);
    expect(confidenceColor(0.75)).toBe(ORANGE);
    expect(confidenceColor(0.76)).toBe(YELLOW);
    expect(confidenceColor(0.89)).toBe(YELLOW);
    expect(confidenceColor(0.9)).toBe(GREEN);
    expect(confidenceColor(1)).toBe(GREEN);
  });

  it('has no colour for an unscored match — absent is not zero', () => {
    expect(confidenceColor(null)).toBeUndefined();
    expect(confidenceColor(undefined)).toBeUndefined();
  });
});

describe('confidencePercent', () => {
  it('rounds to whole percent', () => {
    expect(confidencePercent(0.415)).toBe('42%');
    expect(confidencePercent(1)).toBe('100%');
  });

  it('renders nothing for an unscored match', () => {
    expect(confidencePercent(null)).toBeNull();
  });
});

describe('isLowConfidence', () => {
  it('is true only for a matched release below the threshold', () => {
    expect(isLowConfidence('matched', 0.64)).toBe(true);
    expect(isLowConfidence('matched', LOW_CONFIDENCE - 0.001)).toBe(true);
  });

  it('excludes the threshold itself and anything above it', () => {
    expect(isLowConfidence('matched', LOW_CONFIDENCE)).toBe(false);
    expect(isLowConfidence('matched', 0.94)).toBe(false);
  });

  it('excludes unscored matches and every other status', () => {
    expect(isLowConfidence('matched', null)).toBe(false);
    expect(isLowConfidence('suppressed', 0.41)).toBe(false);
    expect(isLowConfidence('exhausted', 0.62)).toBe(false);
    expect(isLowConfidence('unmatched', 0.1)).toBe(false);
    expect(isLowConfidence('dead', 0.1)).toBe(false);
  });
});

describe('releaseActions', () => {
  const ids = (status: Parameters<typeof releaseActions>[0], confidence: number | null) =>
    releaseActions(status, confidence).map((action) => action.id);

  it('offers hand match, requeue, and suppress for exhausted', () => {
    expect(ids('exhausted', 0.62)).toEqual(['match', 'requeue', 'suppress']);
    expect(releaseActions('exhausted', 0.62)[0]?.primary).toBe(true);
    // Suppress is the discard: rendered apart from and unlike the
    // constructive actions.
    expect(releaseActions('exhausted', 0.62)[2]?.discard).toBe(true);
  });

  it('offers only hand match for suppressed — there is no unsuppress', () => {
    expect(ids('suppressed', 0.41)).toEqual(['match']);
  });

  it('leads with affirm for a low-confidence match', () => {
    const actions = releaseActions('matched', 0.52);
    expect(actions.map((a) => a.id)).toEqual(['affirm', 'match']);
    expect(actions[0]?.primary).toBe(true);
    expect(actions[1]?.primary).toBe(false);
    expect(actions[1]?.label).toBe('Correct match');
  });

  it('offers correct match alone for a trusted match', () => {
    const actions = releaseActions('matched', 0.94);
    expect(actions.map((a) => a.id)).toEqual(['match']);
    expect(actions[0]?.primary).toBe(true);
  });

  it('offers nothing for unmatched (still queued) or dead (downloader-owned)', () => {
    expect(ids('unmatched', null)).toEqual([]);
    expect(ids('dead', null)).toEqual([]);
  });
});

describe('seriesRefOf', () => {
  it('drops the episode fragment', () => {
    expect(seriesRefOf('tvdb:424536#s01e28')).toBe('tvdb:424536');
    expect(seriesRefOf('tvdb:424536')).toBe('tvdb:424536');
  });

  it('is empty for an absent ref', () => {
    expect(seriesRefOf(null)).toBe('');
    expect(seriesRefOf(undefined)).toBe('');
    expect(seriesRefOf('')).toBe('');
  });
});

describe('series title join', () => {
  const rows: ListRow[] = [
    row('tvdb:424536', '葬送のフリーレン'),
    row('tvdb:81189', 'Cowboy Bebop'),
    // Untracked rows carry no ref and must not land in the index.
    { ...row('', 'Nameless'), ref: undefined },
  ];
  const index = seriesTitleIndex(rows);

  it('indexes only rows that carry a ref', () => {
    expect(index.size).toBe(2);
    expect(index.get('tvdb:81189')).toBe('Cowboy Bebop');
  });

  it('joins an episode-level ref against its series', () => {
    expect(seriesTitleForRef(index, 'tvdb:424536#s01e28')).toBe('葬送のフリーレン');
  });

  it('has no title for an unmatched release or a series outside the library', () => {
    expect(seriesTitleForRef(index, '')).toBeUndefined();
    expect(seriesTitleForRef(index, null)).toBeUndefined();
    expect(seriesTitleForRef(index, 'tvdb:999999#s01e01')).toBeUndefined();
  });
});

function row(ref: string, title: string): ListRow {
  return {
    ref,
    title,
    status: 'complete',
    seasonsAvailable: 1,
    seasonCount: 1,
    episodesAvailable: 1,
    episodeCount: 1,
  };
}
