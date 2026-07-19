import { describe, expect, it } from 'vitest';

import { parseMediaPath } from './mediaPath';

describe('parseMediaPath', () => {
  it('parses a series selector with the fallback directory placeholder', () => {
    expect(parseMediaPath('series:Season 1/Example - S01E03.mkv')).toEqual({
      scheme: 'series',
      rel: 'Season 1/Example - S01E03.mkv',
      fileName: 'Example - S01E03.mkv',
      ext: 'mkv',
      portable: `\${KURA_LIBRARY_ROOT}/<series-directory>/Season 1/Example - S01E03.mkv`,
    });
  });

  it('substitutes the real series directory', () => {
    expect(parseMediaPath('series:Season 1/Example.mkv', 'Frieren').portable).toBe(
      `\${KURA_LIBRARY_ROOT}/Frieren/Season 1/Example.mkv`,
    );
  });

  it('parses an inbox selector', () => {
    expect(parseMediaPath('inbox:downloads/Example.tar.mkv', 'ignored')).toEqual({
      scheme: 'inbox',
      rel: 'downloads/Example.tar.mkv',
      fileName: 'Example.tar.mkv',
      ext: 'mkv',
      portable: `\${KURA_INBOX_ROOT}/downloads/Example.tar.mkv`,
    });
  });

  it('keeps an untagged path readable without assigning the wrong root', () => {
    expect(parseMediaPath('downloads/Example.mkv')).toEqual({
      scheme: '',
      rel: 'downloads/Example.mkv',
      fileName: 'Example.mkv',
      ext: 'mkv',
      portable: 'downloads/Example.mkv',
    });
  });

  it('handles a trailing slash without inventing a file name or extension', () => {
    expect(parseMediaPath('series:Season 1/', 'Frieren')).toEqual({
      scheme: 'series',
      rel: 'Season 1/',
      fileName: '',
      ext: '',
      portable: `\${KURA_LIBRARY_ROOT}/Frieren/Season 1/`,
    });
  });
});
