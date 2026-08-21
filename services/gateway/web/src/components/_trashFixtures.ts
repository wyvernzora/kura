import type { TrashCompanion, TrashEntry, TrashList, TrashSeriesEntry } from '@/api/types.gen';

/**
 * Trash fixtures for the `/trash` stories and the Storybook fetch mock.
 *
 * Timestamps are derived from a fixed `FIXTURE_NOW` rather than the
 * wall clock so the rendered ages, stamps and sort order are identical
 * on every run — a story whose ordering drifts by time of day is not a
 * reviewable reference.
 */

export const FIXTURE_NOW = Date.parse('2026-08-20T18:00:00Z');

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

function companion(name: string, sizeBytes: number): TrashCompanion {
  return { path: `series:${name}`, sizeBytes };
}

function entry(opts: {
  id: string;
  episode: string;
  agedDays: number;
  file: string;
  source: string;
  resolution: string;
  megabytes: number;
  companions?: TrashCompanion[];
}): TrashEntry {
  return {
    id: opts.id,
    episode: opts.episode,
    trashedAt: new Date(FIXTURE_NOW - opts.agedDays * DAY).toISOString(),
    mediaPath: `series:Season 1/${opts.file}`,
    source: opts.source,
    resolution: opts.resolution,
    sizeBytes: Math.round(opts.megabytes * 1_000_000),
    companions: opts.companions,
  };
}

function group(directory: string, ref: string, entries: TrashEntry[]): TrashSeriesEntry {
  return {
    directory,
    ref,
    entries,
    bytes: entries.reduce(
      (total, e) => total + e.sizeBytes + (e.companions ?? []).reduce((n, c) => n + c.sizeBytes, 0),
      0,
    ),
  };
}

/**
 * Entries arrive ULID-ascending (oldest first) from the server, which
 * is the order the detail panel reverses.
 */
export const FIXTURE_TRASH_GROUPS: TrashSeriesEntry[] = [
  group('葬送のフリーレン', 'tvdb:424536', [
    entry({
      id: '01K2X7A0B1C2D3E4F5G6H7J8K9',
      episode: 'S02E0003',
      agedDays: 41,
      file: 'Frieren - S02E03 [WebRip-1080p].mkv',
      source: 'WebRip',
      resolution: '1080p',
      megabytes: 1340,
      companions: [companion('S02E03.en.ass', 118_000)],
    }),
    entry({
      id: '01K3JH2M5R8P0Q4WA9B7C6D5E3',
      episode: 'S01E0014',
      agedDays: 3,
      file: 'Frieren - S01E14 - Auras Guillotine [TVRip-480p].mkv',
      source: 'TVRip',
      resolution: '480p',
      megabytes: 402,
    }),
    entry({
      id: '01K3M8QZ4T9V2X7YB0C1D2E3F4',
      episode: 'S01E0014',
      agedDays: 0.08,
      file: 'Frieren - S01E14 - Auras Guillotine [WebRip-720p].mkv',
      source: 'WebRip',
      resolution: '720p',
      megabytes: 812,
      companions: [companion('S01E14.en.srt', 62_400), companion('S01E14.nfo', 2_100)],
    }),
  ]),
  group('進撃の巨人', 'tvdb:267440', [
    entry({
      id: '01K0M7N6P5Q4R3S2T1U0V9W8X7',
      episode: 'S04E0002',
      agedDays: 60,
      file: 'Attack on Titan - S04E02 [TV-720p].mkv',
      source: 'TV',
      resolution: '720p',
      megabytes: 704,
      companions: [companion('S04E02.en.srt', 39_200), companion('S04E02.nfo', 2_400)],
    }),
    entry({
      id: '01K1P8Q7R6S5T4U3V2W1X0Y9Z8',
      episode: 'S04E0001',
      agedDays: 34,
      file: 'Attack on Titan - S04E01 [TV-720p].mkv',
      source: 'TV',
      resolution: '720p',
      megabytes: 690,
    }),
    entry({
      id: '01K2Z9Y8X7W6V5U4T3S2R1Q0P9',
      episode: 'S03E0013',
      agedDays: 12,
      file: 'Attack on Titan - S03E13 [WebRip-1080p].mkv',
      source: 'WebRip',
      resolution: '1080p',
      megabytes: 1210,
    }),
    entry({
      id: '01K3B4C5D6E7F8G9H0J1K2M3N4',
      episode: 'S03E0012',
      agedDays: 12,
      file: 'Attack on Titan - S03E12 [WebRip-1080p].mkv',
      source: 'WebRip',
      resolution: '1080p',
      megabytes: 1180,
      companions: [companion('S03E12.en.srt', 44_800)],
    }),
  ]),
  group('呪術廻戦', 'tvdb:377543', [
    entry({
      id: '01K3D2E3F4G5H6J7K8M9N0P1Q2',
      episode: 'S02E0020',
      agedDays: 6,
      file: 'Jujutsu Kaisen - S02E20 [HDTV-720p].mkv',
      source: 'HDTV',
      resolution: '720p',
      megabytes: 748,
      companions: [companion('S02E20.en.srt', 41_100)],
    }),
    entry({
      id: '01K3N9P8Q7R6S5T4U3V2W1X0Y9',
      episode: 'S02E0019',
      agedDays: 0.2,
      file: 'Jujutsu Kaisen - S02E19 [WebRip-1080p].mkv',
      source: 'WebRip',
      resolution: '1080p',
      megabytes: 1420,
    }),
  ]),
  group('Cowboy Bebop', 'tvdb:76885', [
    entry({
      id: '01JY7L6M5N4P3Q2R1S0T9U8V7W',
      episode: 'S01E0006',
      agedDays: 124,
      file: 'Cowboy Bebop - S01E06 [DVDRip-480p].mkv',
      source: 'DVDRip',
      resolution: '480p',
      megabytes: 498,
      companions: [companion('S01E06.en.srt', 33_900)],
    }),
    entry({
      id: '01JZ8M7N6P5Q4R3S2T1U0V9W8X',
      episode: 'S01E0005',
      agedDays: 90,
      file: 'Cowboy Bebop - S01E05 [DVDRip-480p].mkv',
      source: 'DVDRip',
      resolution: '480p',
      megabytes: 512,
    }),
  ]),
  group('Monster', 'tvdb:78500', [
    entry({
      id: '01JT4K3M2N1P0Q9R8S7T6U5V4W',
      episode: 'S01E0031',
      agedDays: 212,
      file: 'Monster - S01E31 [TVRip-480p].mkv',
      source: 'TVRip',
      resolution: '480p',
      megabytes: 386,
    }),
  ]),
  // Deep-trash case: a full re-grab of a long-running series, so the
  // detail panel has to scroll and the size sort has an obvious winner.
  group(
    'ONE PIECE',
    'tvdb:81797',
    Array.from({ length: 24 }, (_, i) => {
      const resolution = i % 5 === 0 ? '720p' : '1080p';
      const source = i % 7 === 0 ? 'HDTV' : 'WebRip';
      return entry({
        id: `01K2${String(i).padStart(2, '0')}A1B2C3D4E5F6G7H8J9K${i % 10}`,
        episode: `S21E${String(i + 12).padStart(4, '0')}`,
        agedDays: 4 + i * 2.6,
        file: `One Piece - ${1052 + i} [${source}-${resolution}].mkv`,
        source,
        resolution,
        megabytes: (resolution === '720p' ? 640 : 1020) + i * 3.4,
        companions:
          i % 3 === 0 ? [companion(`One Piece - ${1052 + i}.en.srt`, 38_000 + i * 120)] : undefined,
      });
    }),
  ),
];

export function trashListFixture(groups: TrashSeriesEntry[] = FIXTURE_TRASH_GROUPS): TrashList {
  return {
    series: groups,
    totalEntries: groups.reduce((n, g) => n + g.entries.length, 0),
    totalBytes: groups.reduce((n, g) => n + g.bytes, 0),
  };
}
