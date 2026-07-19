import { describe, expect, it } from 'vitest';

import type { MediaShow } from '@/api/types';
import { diffMedia } from './mediaDiff';

const BASE_MEDIA: MediaShow = {
  file: 'series:Season 1/Example - S01E01.mkv',
  source: 'WebRip',
  resolution: '1080p',
  codec: 'H.264',
  size: 1024,
  companions: [],
};

describe('diffMedia', () => {
  it('reports a companion count change', () => {
    const to = {
      ...BASE_MEDIA,
      companions: [
        {
          path: 'inbox:downloads/Example - S01E01.en.ass',
          size: 512,
          mtime: '2026-07-18T00:00:00Z',
        },
      ],
    };

    expect(diffMedia(BASE_MEDIA, to)).toContainEqual({
      label: 'Companions',
      from: '0',
      to: '1',
    });
  });

  it('reports a same-count companion replacement', () => {
    const from = {
      ...BASE_MEDIA,
      companions: [
        {
          path: 'series:Season 1/Example - S01E01.en.ass',
          size: 512,
          mtime: '2026-07-17T00:00:00Z',
        },
      ],
    };
    const to = {
      ...BASE_MEDIA,
      companions: [
        {
          path: 'inbox:downloads/Example - S01E01.en.ass',
          size: 640,
          mtime: '2026-07-18T00:00:00Z',
        },
      ],
    };

    expect(diffMedia(from, to)).toContainEqual({
      label: 'Companions',
      note: 'replaced or changed',
    });
  });

  it('reports a same-count companion metadata change', () => {
    const companion = {
      path: 'series:Season 1/Example - S01E01.ass',
      role: 'subtitle',
      language: 'en',
      label: 'Full',
      size: 512,
      mtime: '2026-07-17T00:00:00Z',
    };
    const from = { ...BASE_MEDIA, companions: [companion] };
    const to = {
      ...BASE_MEDIA,
      companions: [{ ...companion, language: 'ja' }],
    };

    expect(diffMedia(from, to)).toContainEqual({
      label: 'Companions',
      note: 'replaced or changed',
    });
  });

  it('returns no rows when media is unchanged', () => {
    const companion = {
      path: 'series:Season 1/Example - S01E01.en.ass',
      size: 512,
      mtime: '2026-07-17T00:00:00Z',
    };
    const from = { ...BASE_MEDIA, companions: [companion], attrs: { group: 'Example' } };
    const to = { ...BASE_MEDIA, companions: [{ ...companion }], attrs: { group: 'Example' } };

    expect(diffMedia(from, to)).toEqual([]);
  });

  it('reports an attribute change', () => {
    const from = { ...BASE_MEDIA, attrs: { group: 'Old' } };
    const to = { ...BASE_MEDIA, attrs: { group: 'New' } };

    expect(diffMedia(from, to)).toContainEqual({
      label: 'Attributes',
      note: 'added, removed, or changed',
    });
  });

  it('reports resolution, codec, file size, and source changes', () => {
    const to = {
      ...BASE_MEDIA,
      source: 'BluRay',
      resolution: '4K',
      codec: 'HEVC',
      size: 2048,
    };

    expect(diffMedia(BASE_MEDIA, to)).toEqual([
      { label: 'Resolution', from: '1080p', to: '4K' },
      { label: 'Codec', from: 'H.264', to: 'HEVC' },
      { label: 'File size', from: '1 KB', to: '2 KB' },
      { label: 'Source', from: 'WebRip', to: 'BluRay' },
    ]);
  });
});
