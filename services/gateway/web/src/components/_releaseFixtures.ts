import type {
  MatchEvent,
  MatchStatus,
  QueueStats,
  RawItem,
  ReleaseDetail,
  ReleaseItem,
  ReleaseList,
} from '@/api/releaseTypes';
import type { ListRow } from '@/api/types';
import { LOW_CONFIDENCE } from '@/lib/releases';

/**
 * Release fixtures for the `/releases` stories and the Storybook fetch
 * mock.
 *
 * Timestamps derive from a fixed `FIXTURE_NOW` rather than the wall
 * clock so rendered ages, stamps and the newest-first ordering are
 * identical on every run — a story whose ordering drifts by time of
 * day is not a reviewable reference.
 *
 * `releasePage` reproduces the endpoint's own filtering (status,
 * maxConfidence, cursor, limit) so the mocked stories exercise the
 * real two-stream merge instead of a pre-merged array.
 */

export const FIXTURE_NOW = Date.parse('2026-08-20T18:00:00Z');

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

const ago = (ms: number) => new Date(FIXTURE_NOW - ms).toISOString();

interface Seed {
  infohash: string;
  title: string;
  sources: string[];
  agedMs: number;
  status: MatchStatus;
  attempts: number;
  bytes: number | null;
  confidence: number | null;
  ref?: string;
  postings: { title: string; source: string; url: string | null; agedMs: number }[];
  events: {
    status: MatchStatus;
    agedMs: number;
    ref?: string;
    confidence?: number;
    reason?: string;
  }[];
}

/**
 * Infohashes are 40-hex btih, the indexer's canonical release identity
 * — the detail header renders one verbatim.
 */
const SEEDS: Seed[] = [
  {
    infohash: 'a3c21f0b7d4e5f60819a2b3c4d5e6f7081920a3b',
    title: '[SubsPlease] Sousou no Frieren - 29 (1080p) [A3C21F0B].mkv',
    sources: ['nyaa', 'animetosho'],
    agedMs: 5 * HOUR,
    status: 'suppressed',
    attempts: 3,
    bytes: 1_412_000_000,
    confidence: 0.41,
    postings: [
      {
        title: '[SubsPlease] Sousou no Frieren - 29 (1080p) [A3C21F0B].mkv',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1889321',
        agedMs: 5 * HOUR,
      },
      {
        title: '[SubsPlease] Sousou no Frieren - 29 (1080p)',
        source: 'animetosho',
        url: 'https://animetosho.org/view/subsplease-1889321',
        agedMs: 5 * HOUR - 22 * 60_000,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 5 * HOUR },
      {
        status: 'suppressed',
        agedMs: 4 * HOUR,
        confidence: 0.41,
        reason: 'title parsed to season 2 but no season 2 in library',
      },
      {
        status: 'suppressed',
        agedMs: 90 * 60_000,
        confidence: 0.41,
        reason: 'below match threshold (0.75)',
      },
    ],
  },
  {
    infohash: 'b1d0c2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9',
    title: 'Attack on Titan S04E29 The Final Chapters Part 2 1080p WEB-DL AAC2.0 H.264',
    sources: ['nyaa'],
    agedMs: 19 * HOUR,
    status: 'exhausted',
    attempts: 8,
    bytes: 3_980_000_000,
    confidence: 0.62,
    postings: [
      {
        title: 'Attack on Titan S04E29 The Final Chapters Part 2 1080p WEB-DL AAC2.0 H.264',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1888744',
        agedMs: 19 * HOUR,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 19 * HOUR },
      { status: 'unmatched', agedMs: 16 * HOUR, reason: 'lease expired without resolution' },
      {
        status: 'exhausted',
        agedMs: 11 * HOUR,
        confidence: 0.62,
        reason: 'attempt limit reached (8) — special/OVA numbering unresolved',
      },
    ],
  },
  {
    infohash: 'c2e1d0f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9',
    title: '【推しの子】 第2期 第11話 [1920x1080 x264 AAC]',
    sources: ['nyaa', 'animetosho'],
    agedMs: 1.4 * DAY,
    status: 'suppressed',
    attempts: 2,
    bytes: null,
    confidence: null,
    postings: [
      {
        title: '【推しの子】 第2期 第11話 [1920x1080 x264 AAC]',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1887210',
        agedMs: 1.4 * DAY,
      },
      {
        title: 'Oshi no Ko S2 - 11 [1080p]',
        source: 'animetosho',
        url: 'https://animetosho.org/view/1887210',
        agedMs: 1.3 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 1.4 * DAY },
      {
        status: 'suppressed',
        agedMs: 1.2 * DAY,
        reason: 'no romanised title in posting; parser produced no candidate',
      },
    ],
  },
  {
    infohash: 'd3f2e1a0b5c4d7e6f9a8b1c0d3e2f5a4b7c6d9e8',
    title: 'Jujutsu.Kaisen.S02E20.2160p.NF.WEB-DL.DDP5.1.HDR.H.265-Kitsune',
    sources: ['animetosho'],
    agedMs: 2.1 * DAY,
    status: 'exhausted',
    attempts: 8,
    bytes: 6_240_000_000,
    confidence: 0.71,
    postings: [
      {
        title: 'Jujutsu.Kaisen.S02E20.2160p.NF.WEB-DL.DDP5.1.HDR.H.265-Kitsune',
        source: 'animetosho',
        url: 'https://animetosho.org/view/1886004',
        agedMs: 2.1 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 2.1 * DAY },
      {
        status: 'exhausted',
        agedMs: 1.9 * DAY,
        confidence: 0.71,
        reason: 'attempt limit reached (8) — absolute vs season numbering conflict',
      },
    ],
  },
  {
    infohash: 'e4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5',
    title: '[Erai-raws] Kimetsu no Yaiba - Hashira Geiko Hen - 09 [1080p][Multiple Subtitle]',
    sources: ['nyaa'],
    agedMs: 2.6 * DAY,
    status: 'suppressed',
    attempts: 1,
    bytes: 1_390_000_000,
    confidence: 0.68,
    postings: [
      {
        title: '[Erai-raws] Kimetsu no Yaiba - Hashira Geiko Hen - 09 [1080p][Multiple Subtitle]',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1885330',
        agedMs: 2.6 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 2.6 * DAY },
      {
        status: 'suppressed',
        agedMs: 2.5 * DAY,
        confidence: 0.68,
        reason: 'arc title not present in series aliases',
      },
    ],
  },
  {
    infohash: 'f5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6',
    title: 'Vinland.Saga.S02.1080p.BluRay.REMUX.AVC.FLAC.2.0-ZR- [BATCH]',
    sources: ['nyaa', 'animetosho'],
    agedMs: 3.2 * DAY,
    status: 'suppressed',
    attempts: 4,
    bytes: 214_000_000_000,
    confidence: 0.55,
    postings: [
      {
        title: 'Vinland.Saga.S02.1080p.BluRay.REMUX.AVC.FLAC.2.0-ZR- [BATCH]',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1883902',
        agedMs: 3.2 * DAY,
      },
      {
        title: 'Vinland Saga S2 BD Remux (batch)',
        source: 'animetosho',
        url: 'https://animetosho.org/view/1883902',
        agedMs: 3.1 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 3.2 * DAY },
      {
        status: 'suppressed',
        agedMs: 3.0 * DAY,
        confidence: 0.55,
        reason: 'batch posting — no single episode target',
      },
    ],
  },
  {
    infohash: '06c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7',
    title: 'Monster (2004) Complete Series 480p DVDRip x264 [Dual Audio]',
    sources: ['nyaa'],
    agedMs: 4.4 * DAY,
    status: 'exhausted',
    attempts: 8,
    bytes: 41_800_000_000,
    confidence: null,
    postings: [
      {
        title: 'Monster (2004) Complete Series 480p DVDRip x264 [Dual Audio]',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1881077',
        agedMs: 4.4 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 4.4 * DAY },
      {
        status: 'exhausted',
        agedMs: 4.0 * DAY,
        reason: 'attempt limit reached (8) — batch, ambiguous episode span',
      },
    ],
  },
  {
    infohash: '17d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8',
    title: '[Judas] Bocchi the Rock! - S01E12 (1080p) [BD]',
    sources: ['animetosho'],
    agedMs: 5.1 * DAY,
    status: 'suppressed',
    attempts: 2,
    bytes: 890_000_000,
    confidence: 0.73,
    postings: [
      {
        title: '[Judas] Bocchi the Rock! - S01E12 (1080p) [BD]',
        source: 'animetosho',
        url: 'https://animetosho.org/view/1879443',
        agedMs: 5.1 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 5.1 * DAY },
      {
        status: 'suppressed',
        agedMs: 5.0 * DAY,
        confidence: 0.73,
        reason: 'below match threshold (0.75)',
      },
    ],
  },
  {
    infohash: '28e7f6a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d0e9',
    title: 'Steins;Gate 0 - 14 [1080p][HEVC][Multi-Sub]',
    sources: ['nyaa'],
    agedMs: 6.3 * DAY,
    status: 'exhausted',
    attempts: 8,
    bytes: 1_020_000_000,
    confidence: 0.49,
    postings: [
      {
        title: 'Steins;Gate 0 - 14 [1080p][HEVC][Multi-Sub]',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1877215',
        agedMs: 6.3 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 6.3 * DAY },
      {
        status: 'exhausted',
        agedMs: 6.0 * DAY,
        confidence: 0.49,
        reason: 'attempt limit reached (8) — sequel title collides with parent series',
      },
    ],
  },
  {
    infohash: '39f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0',
    title: 'Dungeon Meshi - 18 (WEB 1080p AAC) [Netflix]',
    sources: ['animetosho'],
    agedMs: 7.8 * DAY,
    status: 'suppressed',
    attempts: 5,
    bytes: 1_180_000_000,
    confidence: 0.66,
    postings: [
      {
        title: 'Dungeon Meshi - 18 (WEB 1080p AAC) [Netflix]',
        source: 'animetosho',
        url: 'https://animetosho.org/view/1874880',
        agedMs: 7.8 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 7.8 * DAY },
      {
        status: 'suppressed',
        agedMs: 7.5 * DAY,
        confidence: 0.66,
        reason: 'below match threshold (0.75)',
      },
    ],
  },
  {
    infohash: '4a09b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1',
    title: '[SubsPlease] Mushishi Zoku Shou - 07 (720p) [11B4C0DE].mkv',
    sources: ['nyaa'],
    agedMs: 9.2 * DAY,
    status: 'unmatched',
    attempts: 1,
    bytes: 712_000_000,
    confidence: null,
    postings: [
      {
        title: '[SubsPlease] Mushishi Zoku Shou - 07 (720p) [11B4C0DE].mkv',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1872014',
        agedMs: 9.2 * DAY,
      },
    ],
    events: [{ status: 'unmatched', agedMs: 9.2 * DAY }],
  },
  {
    infohash: '5b1ac9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a3b2',
    title: 'Cowboy.Bebop.S01E17.1080p.BluRay.FLAC2.0.x265-NTb',
    sources: ['animetosho'],
    agedMs: 10.5 * DAY,
    status: 'matched',
    attempts: 1,
    bytes: null,
    confidence: 0.64,
    ref: 'tvdb:81189#s01e17',
    postings: [
      {
        title: 'Cowboy.Bebop.S01E17.1080p.BluRay.FLAC2.0.x265-NTb',
        source: 'animetosho',
        url: 'https://animetosho.org/view/1870441',
        agedMs: 10.5 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 10.5 * DAY },
      {
        status: 'matched',
        agedMs: 10.4 * DAY,
        ref: 'tvdb:81189#s01e17',
        confidence: 0.64,
        reason: 'auto-matched — release group naming differs from library title',
      },
    ],
  },
  {
    infohash: '6c2bd0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3',
    title: '[SubsPlease] Sousou no Frieren - 28 (1080p) [7F2A19C4].mkv',
    sources: ['nyaa', 'animetosho'],
    agedMs: 12.1 * DAY,
    status: 'matched',
    attempts: 1,
    bytes: 1_402_000_000,
    confidence: 0.94,
    ref: 'tvdb:424536#s01e28',
    postings: [
      {
        title: '[SubsPlease] Sousou no Frieren - 28 (1080p) [7F2A19C4].mkv',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1867903',
        agedMs: 12.1 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 12.1 * DAY },
      {
        status: 'matched',
        agedMs: 12.0 * DAY,
        ref: 'tvdb:424536#s01e28',
        confidence: 0.94,
        reason: 'auto-matched above threshold',
      },
    ],
  },
  {
    infohash: '7d3ce1f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4',
    title: 'Shingeki no Kyojin - 63 [1080p][WebRip]',
    sources: ['nyaa'],
    agedMs: 14.6 * DAY,
    status: 'matched',
    attempts: 2,
    bytes: 1_190_000_000,
    confidence: 0.52,
    ref: 'tvdb:267440#s04e04',
    postings: [
      {
        title: 'Shingeki no Kyojin - 63 [1080p][WebRip]',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1864220',
        agedMs: 14.6 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 14.6 * DAY },
      { status: 'suppressed', agedMs: 14.4 * DAY, confidence: 0.7, reason: 'absolute numbering' },
      {
        status: 'matched',
        agedMs: 14.2 * DAY,
        ref: 'tvdb:267440#s04e04',
        confidence: 0.52,
        reason: 'auto-matched — absolute numbering guess',
      },
    ],
  },
  {
    infohash: '8e4df2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5',
    title: 'Fullmetal.Alchemist.Brotherhood.S01E64.1080p.BluRay.x264',
    sources: ['animetosho'],
    agedMs: 18.9 * DAY,
    status: 'matched',
    attempts: 1,
    bytes: 2_140_000_000,
    confidence: 0.97,
    ref: 'tvdb:114801#s01e64',
    postings: [
      {
        title: 'Fullmetal.Alchemist.Brotherhood.S01E64.1080p.BluRay.x264',
        source: 'animetosho',
        url: 'https://animetosho.org/view/1859110',
        agedMs: 18.9 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 18.9 * DAY },
      {
        status: 'matched',
        agedMs: 18.8 * DAY,
        ref: 'tvdb:114801#s01e64',
        confidence: 0.97,
        reason: 'auto-matched above threshold',
      },
    ],
  },
  {
    infohash: '9f5ea3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6',
    title: 'Anime Music Video Pack 2015-2019 [MULTI]',
    sources: ['nyaa'],
    agedMs: 24.3 * DAY,
    status: 'dead',
    attempts: 3,
    bytes: 88_400_000_000,
    confidence: null,
    postings: [
      {
        title: 'Anime Music Video Pack 2015-2019 [MULTI]',
        source: 'nyaa',
        url: 'https://nyaa.si/view/1851002',
        agedMs: 24.3 * DAY,
      },
    ],
    events: [
      { status: 'unmatched', agedMs: 24.3 * DAY },
      { status: 'suppressed', agedMs: 24.0 * DAY, reason: 'no episode structure' },
      {
        status: 'dead',
        agedMs: 23.1 * DAY,
        reason: 'operator marked dead — not library content',
      },
    ],
  },
];

function toItem(seed: Seed): ReleaseItem {
  return {
    infohash: seed.infohash,
    ref: seed.ref ?? '',
    title: seed.title,
    sizeBytes: seed.bytes,
    publishedAt: ago(seed.agedMs),
    confidence: seed.confidence,
    sources: seed.sources,
    matchStatus: seed.status,
  };
}

function toDetail(seed: Seed): ReleaseDetail {
  const rawItems: RawItem[] = seed.postings.map((posting, i) => ({
    id: i + 1,
    source: posting.source,
    sourceId: String(1_800_000 + i),
    title: posting.title,
    url: posting.url,
    publishedAt: ago(posting.agedMs),
    ingestedAt: ago(posting.agedMs - 60_000),
  }));
  // Oldest first, the order the endpoint returns them in.
  const matchEvents: MatchEvent[] = seed.events.map((event, i) => ({
    id: i + 1,
    status: event.status,
    ref: event.ref ?? null,
    confidence: event.confidence ?? null,
    reason: event.reason ?? null,
    createdAt: ago(event.agedMs),
  }));
  const matched = seed.events.find((event) => event.status === 'matched');
  return {
    infohash: seed.infohash,
    title: seed.title,
    magnet: `magnet:?xt=urn:btih:${seed.infohash}`,
    sizeBytes: seed.bytes,
    publishedAt: ago(seed.agedMs),
    sources: seed.sources,
    matchStatus: seed.status,
    ref: seed.ref ?? null,
    confidence: seed.confidence,
    firstMatchedAt: matched ? ago(matched.agedMs) : null,
    attemptCount: seed.attempts,
    createdAt: ago(seed.agedMs),
    updatedAt: ago(seed.events[seed.events.length - 1]?.agedMs ?? seed.agedMs),
    rawItems,
    matchEvents,
  };
}

export const FIXTURE_RELEASES: ReleaseItem[] = SEEDS.map(toItem);
export const FIXTURE_RELEASE_DETAILS: ReleaseDetail[] = SEEDS.map(toDetail);

export function releaseDetailFixture(infohash: string): ReleaseDetail | undefined {
  return FIXTURE_RELEASE_DETAILS.find((detail) => detail.infohash === infohash);
}

export const FIXTURE_QUEUE_STATS: QueueStats = {
  available: FIXTURE_RELEASES.filter((r) => r.matchStatus === 'unmatched').length,
  leased: 0,
  unmatched: FIXTURE_RELEASES.filter((r) => r.matchStatus === 'unmatched').length,
  matched: FIXTURE_RELEASES.filter((r) => r.matchStatus === 'matched').length,
  suppressed: FIXTURE_RELEASES.filter((r) => r.matchStatus === 'suppressed').length,
  exhausted: FIXTURE_RELEASES.filter((r) => r.matchStatus === 'exhausted').length,
  dead: FIXTURE_RELEASES.filter((r) => r.matchStatus === 'dead').length,
};

export interface ReleasePageQuery {
  status?: string | null;
  maxConfidence?: string | null;
  cursor?: string | null;
  limit?: string | null;
}

/**
 * Serves one page the way the endpoint does: status filter, optional
 * confidence ceiling, newest-first, opaque cursor. The cursor here is
 * just the offset — the real one encodes (publishedAt, infohash), but
 * the SPA treats it as opaque either way.
 */
export function releasePage(
  query: ReleasePageQuery,
  releases: readonly ReleaseItem[] | undefined = FIXTURE_RELEASES,
): ReleaseList {
  releases = releases ?? FIXTURE_RELEASES;
  const statuses = (query.status ?? 'matched').split(',').filter(Boolean);
  const ceiling = query.maxConfidence == null ? null : Number(query.maxConfidence);
  const limit = Number(query.limit ?? 20) || 20;
  const offset = Number(query.cursor ?? 0) || 0;

  const matching = [...releases]
    .filter((item) => statuses.includes(item.matchStatus))
    .filter((item) => ceiling == null || (item.confidence != null && item.confidence < ceiling))
    .sort((a, b) => Date.parse(b.publishedAt) - Date.parse(a.publishedAt));

  const page = matching.slice(offset, offset + limit);
  const next = offset + limit;
  return {
    items: page,
    ...(next < matching.length ? { nextCursor: String(next) } : {}),
  };
}

/** The low-confidence stream, for stories that assert the merge. */
export const FIXTURE_LOW_CONFIDENCE = FIXTURE_RELEASES.filter(
  (item) =>
    item.matchStatus === 'matched' && item.confidence != null && item.confidence < LOW_CONFIDENCE,
);

/**
 * Library rows the release refs join against, and the corpus the
 * hand-match search runs over. Refs match the fixtures above so a
 * matched release resolves to a series name in the list.
 */
export const FIXTURE_RELEASE_SERIES: ListRow[] = [
  seriesRow('tvdb:424536', '葬送のフリーレン', 'Frieren: Beyond Journey’s End', 28),
  seriesRow('tvdb:267440', '進撃の巨人', 'Attack on Titan', 87),
  seriesRow('tvdb:389597', '【推しの子】', 'Oshi no Ko', 24),
  seriesRow('tvdb:377543', '呪術廻戦', 'Jujutsu Kaisen', 47),
  seriesRow('tvdb:348545', '鬼滅の刃', 'Demon Slayer', 55),
  seriesRow('tvdb:359274', 'Vinland Saga', undefined, 48),
  seriesRow('tvdb:78500', 'Monster', undefined, 74),
  seriesRow('tvdb:415249', 'ぼっち・ざ・ろっく！', 'Bocchi the Rock!', 12),
  seriesRow('tvdb:244061', 'Steins;Gate', undefined, 24),
  seriesRow('tvdb:329805', 'Steins;Gate 0', undefined, 23),
  seriesRow('tvdb:414057', 'ダンジョン飯', 'Delicious in Dungeon', 24),
  seriesRow('tvdb:81189', 'Cowboy Bebop', undefined, 26),
  seriesRow(
    'tvdb:114801',
    '鋼の錬金術師 FULLMETAL ALCHEMIST',
    'Fullmetal Alchemist: Brotherhood',
    64,
  ),
  seriesRow('tvdb:79474', 'Mushishi', undefined, 46),
];

function seriesRow(
  ref: string,
  title: string,
  canonicalTitle: string | undefined,
  episodeCount: number,
): ListRow {
  return {
    ref,
    title,
    canonicalTitle,
    status: 'complete',
    seasonsAvailable: 1,
    seasonCount: 1,
    episodesAvailable: episodeCount,
    episodeCount,
    // The alias fold the client-side fuzzy search leans on; the server
    // builds this per row.
    searchKey: [title, canonicalTitle, ref].filter(Boolean).join('\n'),
  };
}
