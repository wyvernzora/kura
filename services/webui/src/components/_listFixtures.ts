import type { ListRow } from '@/api/types';

/**
 * Hand-crafted ListRow fixtures for the library-grid stories. Mix of
 * statuses + airing/non-airing + with/without metadataRef so a single
 * page demos the visual variants the live grid will render.
 */

function row(opts: {
  metadataRef?: string;
  title: string;
  status: ListRow['status'];
  isAiring?: boolean;
  episodesAvailable?: number;
  episodeCount: number;
  posterUrl?: string;
  tags?: string[];
}): ListRow {
  return {
    metadataRef: opts.metadataRef,
    title: opts.title,
    status: opts.status,
    isAiring: opts.isAiring,
    seasonsAvailable: 1,
    seasonCount: 1,
    episodesAvailable: opts.episodesAvailable ?? opts.episodeCount,
    episodeCount: opts.episodeCount,
    posterUrl: opts.posterUrl,
    tags: opts.tags,
  };
}

export const FIXTURE_LIST_ROWS: ListRow[] = [
  row({
    metadataRef: 'tvdb:424536',
    title: 'Frieren: Beyond Journey’s End',
    status: 'incomplete',
    isAiring: true,
    episodesAvailable: 18,
    episodeCount: 28,
    tags: ['priority:high'],
  }),
  row({
    metadataRef: 'tvdb:81189',
    title: 'Cowboy Bebop',
    status: 'complete',
    episodeCount: 26,
    tags: ['maintenance:disabled'],
  }),
  row({
    metadataRef: 'tvdb:79474',
    title: 'Mushishi',
    status: 'complete',
    episodeCount: 26,
  }),
  row({
    metadataRef: 'tvdb:143471',
    title: 'YAWARA! A Fashionable Judo Girl!',
    status: 'incomplete',
    episodeCount: 124,
    episodesAvailable: 60,
  }),
  row({
    metadataRef: 'tvdb:305074',
    title: 'Re:Zero — Starting Life in Another World',
    status: 'incomplete',
    isAiring: true,
    episodeCount: 75,
    episodesAvailable: 50,
  }),
  row({
    metadataRef: 'tvdb:359274',
    title: 'Vinland Saga',
    status: 'incomplete',
    episodeCount: 48,
    episodesAvailable: 24,
  }),
  row({
    metadataRef: 'tvdb:333495',
    title: 'Houseki no Kuni',
    status: 'complete',
    episodeCount: 12,
  }),
  row({
    title: 'A folder with no metadata yet',
    status: 'untracked',
    episodeCount: 0,
    episodesAvailable: 0,
  }),
  row({
    metadataRef: 'tvdb:99999999',
    title: 'A series whose provider blew up',
    status: 'error',
    episodeCount: 12,
    episodesAvailable: 0,
  }),
  row({
    metadataRef: 'tvdb:73752',
    title: 'Berserk (1997)',
    status: 'complete',
    episodeCount: 25,
  }),
];
