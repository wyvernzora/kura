import type { EpisodeShow, SeasonShow, Show } from '@/api/types';

/**
 * Hand-crafted Show fixtures for Storybook + ad-hoc tests. Kept in
 * one place so visual variants stay consistent across stories.
 */

function media(opts: {
  source: string;
  resolution: string;
  codec: string;
  size: number;
  companions?: number;
}): EpisodeShow['active'] {
  return {
    source: opts.source,
    resolution: opts.resolution,
    codec: opts.codec,
    size: opts.size,
    file: 'Frieren.S01E01.1080p.BluRay.HEVC.mkv',
    companions: Array.from({ length: opts.companions ?? 0 }).map((_, i) => ({
      path: `Frieren.S01E01.${['en', 'jp', 'fr'][i] ?? 'en'}.srt`,
      role: 'subtitle',
      language: ['en', 'jp', 'fr'][i] ?? 'en',
      size: 30_000,
      mtime: '2024-08-01T10:00:00Z',
    })),
  };
}

function presentEp(opts: {
  episode: string;
  title: string;
  aired: string;
  source: string;
  resolution: string;
  size: number;
  companions?: number;
}): EpisodeShow {
  return {
    episode: opts.episode,
    aired: opts.aired,
    status: 'present',
    preferredTitle: opts.title,
    active: media({
      source: opts.source,
      resolution: opts.resolution,
      codec: 'HEVC 10-bit',
      size: opts.size,
      companions: opts.companions,
    }),
  };
}

function missingEp(episode: string, title: string, aired: string): EpisodeShow {
  return { episode, aired, status: 'missing', preferredTitle: title };
}

function pendingEp(episode: string, title: string, aired: string): EpisodeShow {
  return { episode, aired, status: 'pending', preferredTitle: title };
}

const EP_S01E01: EpisodeShow = presentEp({
  episode: 'S01E0001',
  title: '出会い',
  aired: '2023-09-29',
  source: 'BluRay',
  resolution: '1080p',
  size: 1.4 * 1024 ** 3,
  companions: 2,
});
const EP_S01E02: EpisodeShow = presentEp({
  episode: 'S01E0002',
  title: '旅立ち',
  aired: '2023-10-06',
  source: 'BluRay',
  resolution: '1080p',
  size: 1.3 * 1024 ** 3,
});
const EP_S01E03: EpisodeShow = missingEp('S01E0003', '小さな約束', '2023-10-13');
const EP_S01E04: EpisodeShow = presentEp({
  episode: 'S01E0004',
  title: '雨の街',
  aired: '2023-10-20',
  source: 'WebRip',
  resolution: '720p',
  size: 0.6 * 1024 ** 3,
});

const EP_S02E01: EpisodeShow = presentEp({
  episode: 'S02E0001',
  title: '銀の鈴',
  aired: '2026-04-01',
  source: 'Web-DL',
  resolution: '2160p',
  size: 4.1 * 1024 ** 3,
});
const EP_S02E02: EpisodeShow = presentEp({
  episode: 'S02E0002',
  title: '春の終わり',
  aired: '2026-04-08',
  source: 'Web-DL',
  resolution: '1080p',
  size: 1.1 * 1024 ** 3,
  companions: 1,
});
const EP_S02E03: EpisodeShow = pendingEp('S02E0003', '名もなき花', '2026-05-15');

const EP_S00E01: EpisodeShow = presentEp({
  episode: 'S00E0001',
  title: 'OVA: 月明かり',
  aired: '2024-03-01',
  source: 'BluRay',
  resolution: '1080p',
  size: 2.0 * 1024 ** 3,
});
const EP_S00E02: EpisodeShow = missingEp('S00E0002', 'OVA: 砂の城', '2024-04-01');

const seasonOne: SeasonShow = {
  number: 1,
  summary: { episodeCount: 4, present: 3, missing: 1, pending: 0, staged: 0, stagedReplacement: 0 },
  episodes: [EP_S01E01, EP_S01E02, EP_S01E03, EP_S01E04],
};

const seasonTwo: SeasonShow = {
  number: 2,
  summary: { episodeCount: 3, present: 2, missing: 0, pending: 1, staged: 0, stagedReplacement: 0 },
  episodes: [EP_S02E01, EP_S02E02, EP_S02E03],
};

const specials: SeasonShow = {
  number: 0,
  summary: { episodeCount: 2, present: 1, missing: 1, pending: 0, staged: 0, stagedReplacement: 0 },
  episodes: [EP_S00E01, EP_S00E02],
};

export const FIXTURE_SHOW_AIRING: Show = {
  metadataRef: 'tvdb:424536',
  ref: 'Frieren - Beyond Journeys End',
  root: '/library/Frieren - Beyond Journeys End',
  preferredTitle: '葬送のフリーレン',
  canonicalTitle: 'Frieren: Beyond Journey’s End',
  status: 'airing',
  lastScanned: new Date(Date.now() - 4 * 60 * 60 * 1000).toISOString(),
  artwork: {
    poster: {
      url: 'https://artworks.thetvdb.com/banners/v4/series/424536/posters/65b1a59f6b5d2.jpg',
    },
  },
  seasons: [seasonOne, seasonTwo, specials],
};

export const FIXTURE_SHOW_COMPLETE_SINGLE: Show = {
  metadataRef: 'tvdb:81189',
  ref: 'Cowboy Bebop',
  root: '/library/Cowboy Bebop',
  preferredTitle: 'Cowboy Bebop',
  status: 'complete',
  lastScanned: new Date(Date.now() - 32 * 24 * 60 * 60 * 1000).toISOString(),
  seasons: [
    {
      number: 1,
      summary: {
        episodeCount: 2,
        present: 2,
        missing: 0,
        pending: 0,
        staged: 0,
        stagedReplacement: 0,
      },
      episodes: [
        presentEp({
          episode: 'S01E0001',
          title: 'Asteroid Blues',
          aired: '1998-04-03',
          source: 'BluRay',
          resolution: '1080p',
          size: 1.0 * 1024 ** 3,
        }),
        presentEp({
          episode: 'S01E0002',
          title: 'Stray Dog Strut',
          aired: '1998-04-10',
          source: 'BluRay',
          resolution: '1080p',
          size: 1.0 * 1024 ** 3,
        }),
      ],
    },
  ],
};

export const FIXTURE_EPISODE_PRESENT: EpisodeShow = EP_S01E01;
export const FIXTURE_EPISODE_MISSING: EpisodeShow = EP_S01E03;
export const FIXTURE_EPISODE_PENDING: EpisodeShow = EP_S02E03;
export const FIXTURE_SEASON_AIRING: SeasonShow = seasonTwo;
export const FIXTURE_SEASON_SPECIALS: SeasonShow = specials;
