import { describe, expect, it } from 'vitest';

import type { MatchStatus, ReleaseItem } from '@/api/releaseTypes';

import {
  type LoadedStream,
  mergeReleaseStreams,
  nextStreamToLoad,
  type ReleaseFilterKey,
  releaseListPath,
  streamsForFilter,
} from './releaseStreams';

const NOW = Date.parse('2026-08-20T18:00:00Z');
const HOUR = 3_600_000;

function item(infohash: string, agedHours: number, status: MatchStatus = 'matched'): ReleaseItem {
  return {
    infohash,
    ref: '',
    title: infohash,
    sizeBytes: null,
    publishedAt: new Date(NOW - agedHours * HOUR).toISOString(),
    confidence: null,
    sources: ['nyaa'],
    matchStatus: status,
  };
}

const filter = (...keys: ReleaseFilterKey[]) => new Set(keys);

describe('streamsForFilter', () => {
  it('puts plain statuses in one comma-joined stream', () => {
    const streams = streamsForFilter(filter('exhausted', 'suppressed'));
    expect(streams).toHaveLength(1);
    expect(streams[0]?.key).toBe('plain');
    expect(streams[0]?.statuses).toEqual(['suppressed', 'exhausted']);
    expect(streams[0]?.maxConfidence).toBeUndefined();
  });

  it('gives low confidence its own matched + ceiling stream', () => {
    const streams = streamsForFilter(filter('lowconf'));
    expect(streams).toHaveLength(1);
    expect(streams[0]).toEqual({ key: 'lowconf', statuses: ['matched'], maxConfidence: 0.75 });
  });

  it('runs two streams for the default attention view', () => {
    const streams = streamsForFilter(filter('exhausted', 'lowconf'));
    expect(streams.map((s) => s.key)).toEqual(['plain', 'lowconf']);
    expect(streams[0]?.statuses).toEqual(['exhausted']);
  });

  it('names every status for "All" — the endpoint defaults to matched-only', () => {
    const streams = streamsForFilter(new Set());
    expect(streams).toHaveLength(1);
    expect(streams[0]?.statuses).toEqual([
      'suppressed',
      'exhausted',
      'unmatched',
      'matched',
      'dead',
    ]);
  });
});

describe('releaseListPath', () => {
  it('always sends an explicit status', () => {
    expect(releaseListPath({ key: 'plain', statuses: ['exhausted'] }, undefined, 8)).toBe(
      '/api/releases/v1?status=exhausted&limit=8',
    );
  });

  it('sends the confidence ceiling only for the low-confidence stream', () => {
    expect(
      releaseListPath({ key: 'lowconf', statuses: ['matched'], maxConfidence: 0.75 }, undefined, 8),
    ).toBe('/api/releases/v1?status=matched&maxConfidence=0.75&limit=8');
  });

  it('appends the cursor when resuming', () => {
    expect(releaseListPath({ key: 'plain', statuses: ['matched'] }, 'abc def', 8)).toBe(
      '/api/releases/v1?status=matched&limit=8&cursor=abc+def',
    );
  });
});

describe('mergeReleaseStreams', () => {
  it('interleaves newest first across streams', () => {
    const merged = mergeReleaseStreams([
      stream('plain', [item('a', 1), item('c', 5)], false),
      stream('lowconf', [item('b', 3), item('d', 9)], false),
    ]);
    expect(merged.items.map((i) => i.infohash)).toEqual(['a', 'b', 'c', 'd']);
    expect(merged.hasMore).toBe(false);
  });

  it('dedupes by infohash when both streams carry a release', () => {
    const merged = mergeReleaseStreams([
      stream('plain', [item('a', 1), item('shared', 4)], false),
      stream('lowconf', [item('shared', 4)], false),
    ]);
    expect(merged.items.map((i) => i.infohash)).toEqual(['a', 'shared']);
  });

  it('withholds rows a still-paged stream could still overtake', () => {
    // The lowconf stream is loaded only to 3h ago and has more pages,
    // so anything older than that could be outranked by a row that has
    // not arrived yet.
    const merged = mergeReleaseStreams([
      stream('plain', [item('a', 1), item('c', 5), item('e', 20)], false),
      stream('lowconf', [item('b', 3)], true),
    ]);
    expect(merged.items.map((i) => i.infohash)).toEqual(['a', 'b']);
    expect(merged.hasMore).toBe(true);
  });

  it('cuts at the NEWEST frontier when both streams are still paging', () => {
    // plain reaches back to 10h, lowconf only to 3h, and both have more
    // pages. Cutting at plain's older frontier would show `c` even
    // though an unfetched lowconf row from 5h ago belongs above it.
    const merged = mergeReleaseStreams([
      stream('plain', [item('a', 1), item('c', 10)], true),
      stream('lowconf', [item('b', 3)], true),
    ]);
    expect(merged.items.map((i) => i.infohash)).toEqual(['a', 'b']);
  });

  it('withholds everything while a paged stream has loaded nothing', () => {
    const merged = mergeReleaseStreams([
      stream('plain', [item('a', 1)], false),
      stream('lowconf', [], true),
    ]);
    expect(merged.items).toEqual([]);
    expect(merged.hasMore).toBe(true);
  });

  it('emits everything once every stream is complete', () => {
    const merged = mergeReleaseStreams([
      stream('plain', [item('a', 1), item('c', 30)], false),
      stream('lowconf', [item('b', 3)], false),
    ]);
    expect(merged.items.map((i) => i.infohash)).toEqual(['a', 'b', 'c']);
  });

  it('breaks equal timestamps on infohash DESC, matching the server page order', () => {
    // The server seeks with (key, infohash) < (cursor), so a later page's
    // equal-timestamp rows have smaller infohashes; sorting them above
    // already-rendered rows would reorder the list under the user.
    const merged = mergeReleaseStreams([
      stream('plain', [item('a', 4), item('z', 4)], false),
      stream('lowconf', [], false),
    ]);
    expect(merged.items.map((i) => i.infohash)).toEqual(['z', 'a']);
  });
});

describe('nextStreamToLoad', () => {
  it('advances the stream whose frontier is newest — it sets the cutoff', () => {
    expect(
      nextStreamToLoad([
        stream('plain', [item('a', 1), item('c', 20)], true),
        stream('lowconf', [item('b', 3)], true),
      ]),
    ).toBe('lowconf');
  });

  it('skips a completed stream', () => {
    expect(
      nextStreamToLoad([
        stream('plain', [item('a', 1)], false),
        stream('lowconf', [item('b', 30)], true),
      ]),
    ).toBe('lowconf');
  });

  it('prioritises a paged stream with nothing loaded', () => {
    expect(
      nextStreamToLoad([stream('plain', [item('a', 1)], true), stream('lowconf', [], true)]),
    ).toBe('lowconf');
  });

  it('is null when nothing more can be fetched', () => {
    expect(
      nextStreamToLoad([
        stream('plain', [item('a', 1)], false),
        stream('lowconf', [item('b', 3)], false),
      ]),
    ).toBeNull();
    expect(nextStreamToLoad([])).toBeNull();
  });
});

function stream(key: LoadedStream['key'], items: ReleaseItem[], hasMore: boolean): LoadedStream {
  return { key, items, hasMore };
}
