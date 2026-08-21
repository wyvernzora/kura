import { describe, expect, it } from 'vitest';

import { composeReleaseRef, formatEpisodeMarker, parseEpisodeMarker } from './releaseEpisode';

describe('parseEpisodeMarker', () => {
  it('reads SxxEyy in any case and with any separator', () => {
    expect(parseEpisodeMarker('Attack on Titan S04E29 The Final Chapters 1080p')).toEqual({
      season: 4,
      episode: 29,
    });
    expect(parseEpisodeMarker('Jujutsu.Kaisen.S02E20.2160p.NF.WEB-DL')).toEqual({
      season: 2,
      episode: 20,
    });
    expect(parseEpisodeMarker('[Judas] Bocchi the Rock! - s01e12 (1080p) [BD]')).toEqual({
      season: 1,
      episode: 12,
    });
    expect(parseEpisodeMarker('Some Show S3 E7 [1080p]')).toEqual({ season: 3, episode: 7 });
  });

  it('reads a spaced episode number as season 1', () => {
    expect(
      parseEpisodeMarker('[SubsPlease] Sousou no Frieren - 29 (1080p) [A3C21F0B].mkv'),
    ).toEqual({ season: 1, episode: 29 });
    expect(parseEpisodeMarker('Steins;Gate 0 - 14 [1080p][HEVC]')).toEqual({
      season: 1,
      episode: 14,
    });
    expect(parseEpisodeMarker('Dungeon Meshi - 18')).toEqual({ season: 1, episode: 18 });
  });

  it('takes the LAST spaced number — a leading cour number is not the episode', () => {
    // Two numeric candidates: the `- 2 -` cour marker and the episode.
    expect(parseEpisodeMarker('Mobile Suit Gundam 00 - 2 - 15 [720p]')).toEqual({
      season: 1,
      episode: 15,
    });
    expect(
      parseEpisodeMarker('[Erai-raws] Kimetsu no Yaiba - Hashira Geiko Hen - 09 [1080p]'),
    ).toEqual({ season: 1, episode: 9 });
  });

  it('reads 第N話, with 第N期 setting the season', () => {
    expect(parseEpisodeMarker('【推しの子】 第2期 第11話 [1920x1080 x264 AAC]')).toEqual({
      season: 2,
      episode: 11,
    });
    expect(parseEpisodeMarker('とある作品 第7話')).toEqual({ season: 1, episode: 7 });
  });

  it('prefers SxxEyy over the other shapes when both are present', () => {
    expect(parseEpisodeMarker('Show - 63 [WebRip] S04E04')).toEqual({ season: 4, episode: 4 });
  });

  it('does not read a year as an episode number', () => {
    expect(parseEpisodeMarker('Anime Music Video Pack - 2019 [MULTI]')).toBeNull();
    expect(parseEpisodeMarker('Monster (2004) Complete Series 480p DVDRip x264')).toBeNull();
  });

  it('returns null for a season-only batch — it is not an episode', () => {
    expect(parseEpisodeMarker('Vinland.Saga.S02.1080p.BluRay.REMUX.AVC-ZR- [BATCH]')).toBeNull();
    expect(parseEpisodeMarker('Fullmetal Alchemist Complete Series [BD]')).toBeNull();
  });

  it('ignores a hyphen that is not spaced on both sides', () => {
    expect(parseEpisodeMarker('Anime Music Video Pack 2015-2019 [MULTI]')).toBeNull();
    expect(parseEpisodeMarker('Some-12 Show [1080p]')).toBeNull();
  });
});

describe('formatEpisodeMarker', () => {
  it('zero-pads both numbers to two digits', () => {
    expect(formatEpisodeMarker({ season: 1, episode: 9 })).toBe('s01e09');
    expect(formatEpisodeMarker({ season: 4, episode: 29 })).toBe('s04e29');
  });

  it('keeps wider episode numbers whole', () => {
    expect(formatEpisodeMarker({ season: 21, episode: 1052 })).toBe('s21e1052');
  });
});

describe('composeReleaseRef', () => {
  it('appends the episode fragment to the series ref', () => {
    expect(composeReleaseRef('tvdb:424536', { season: 1, episode: 29 })).toBe('tvdb:424536#s01e29');
  });

  it('submits the series-level ref when nothing parsed', () => {
    expect(composeReleaseRef('tvdb:359274', null)).toBe('tvdb:359274');
  });

  it('replaces an existing fragment rather than stacking one', () => {
    expect(composeReleaseRef('tvdb:267440#s04e04', { season: 4, episode: 5 })).toBe(
      'tvdb:267440#s04e05',
    );
    expect(composeReleaseRef('tvdb:267440#s04e04', null)).toBe('tvdb:267440');
  });
});
