import { describe, expect, it } from 'vitest';

import {
  EPISODE_STATUS_DOT_BG,
  EPISODE_STATUS_LABEL,
  episodeSubText,
  isDimmedStatus,
  isSubOptimalResolution,
  isSubOptimalSource,
  resolutionBucket,
  sourceBucket,
} from './episodeStatus';

describe('episode status maps', () => {
  it('covers every status value with a label and dot color', () => {
    for (const s of ['present', 'staged', 'staged_replacement', 'missing', 'pending'] as const) {
      expect(EPISODE_STATUS_LABEL[s]).toBeTruthy();
      expect(EPISODE_STATUS_DOT_BG[s]).toBeTruthy();
    }
  });

  it('only pending rows render dimmed', () => {
    expect(isDimmedStatus('pending')).toBe(true);
    expect(isDimmedStatus('present')).toBe(false);
    expect(isDimmedStatus('missing')).toBe(false);
    expect(isDimmedStatus('staged')).toBe(false);
    expect(isDimmedStatus('staged_replacement')).toBe(false);
  });
});

describe('episodeSubText', () => {
  it('annotates missing + pending; everything else is null', () => {
    expect(episodeSubText('missing')).toBe('no file on disk');
    expect(episodeSubText('pending')).toBe('not yet aired');
    expect(episodeSubText('present')).toBeNull();
    expect(episodeSubText('staged')).toBeNull();
    expect(episodeSubText('staged_replacement')).toBeNull();
  });
});

describe('isSubOptimalSource', () => {
  it.each(['BluRay', 'Web-DL', 'WebDL'])('keeps %s premium (outlined)', (source) => {
    expect(isSubOptimalSource(source)).toBe(false);
  });

  it.each([
    'WebRip',
    'TV',
    'TVRip',
    'HDTV',
    'DVDRip',
    'Unknown',
  ])('flags %s as sub-optimal', (source) => {
    expect(isSubOptimalSource(source)).toBe(true);
  });
});

describe('isSubOptimalResolution', () => {
  it.each(['4K', '2160p', '1080p'])('keeps %s premium', (res) => {
    expect(isSubOptimalResolution(res)).toBe(false);
  });

  it.each(['720p', '480p', '360p', '???'])('flags %s as sub-optimal', (res) => {
    expect(isSubOptimalResolution(res)).toBe(true);
  });
});

describe('sourceBucket', () => {
  it.each([
    ['BluRay', 'airing'],
    ['Web-DL', 'complete'],
    ['WebDL', 'complete'],
    ['WebRip', 'incomplete'],
    ['TV', 'error'],
    ['TVRip', 'error'],
    ['HDTV', 'error'],
    ['DVDRip', 'error'],
    ['Unknown', 'untracked'],
    ['weird-fallback', 'untracked'],
  ] as const)('%s → %s', (source, bucket) => {
    expect(sourceBucket(source)).toBe(bucket);
  });
});

describe('resolutionBucket', () => {
  it.each([
    ['4K', 'airing'],
    ['2160p', 'airing'],
    ['1080p', 'complete'],
    ['720p', 'incomplete'],
    ['480p', 'incomplete'],
    ['360p', 'error'],
    ['', 'error'],
  ] as const)('%s → %s', (res, bucket) => {
    expect(resolutionBucket(res)).toBe(bucket);
  });
});
