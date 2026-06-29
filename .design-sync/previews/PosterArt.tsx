import * as React from 'react';
import { PosterArt } from '@ds-stories/web/src/poster/PosterArt';

// Owned preview. The Storybook decorators size PosterArt with arbitrary-value
// Tailwind utilities (aspect-[0.7], w-[180px], grid-cols-4, shadow-poster) that
// are NOT present in the converter's compiled CSS (JIT only emits classes it
// scrapes from app source, not story-only decorators). Without the sized,
// positioned container the absolutely-positioned SVG stretches to fill the
// preview cell. Mirror the decorators with inline styles so sizing is honored.

const POSTER_SHADOW =
  '0 1px 2px rgba(15,23,42,0.08), 0 8px 24px rgba(15,23,42,0.12)';

function single(title: string) {
  return () => (
    <div
      style={{
        position: 'relative',
        width: 180,
        aspectRatio: '0.7',
        overflow: 'hidden',
        borderRadius: 12,
        boxShadow: POSTER_SHADOW,
      }}
    >
      <PosterArt title={title} />
    </div>
  );
}

export const ShortLatin = /* Short Latin */ single('Frieren');
export const LongLatin = /* Long Latin */ single('Re:Zero — Starting Life in Another World');
export const Japanese = /* Japanese */ single('葬送のフリーレン');
export const TraditionalChinese = /* Traditional Chinese */ single('葬送的芙莉蓮');
export const Numeric = /* Numeric */ single('86 — Eighty Six');

const GALLERY_TITLES = [
  'Frieren',
  'Cowboy Bebop',
  'Re:Zero',
  'Mushishi',
  'Houseki no Kuni',
  'Vinland Saga',
  'Berserk',
  '86',
];

export const Gallery = /* Gallery */ () => (
  <div
    style={{
      display: 'grid',
      gridTemplateColumns: 'repeat(4, max-content)',
      gap: 12,
    }}
  >
    {GALLERY_TITLES.map((title) => (
      <div key={title} style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <div
          style={{
            position: 'relative',
            width: 140,
            aspectRatio: '0.7',
            overflow: 'hidden',
            borderRadius: 12,
            boxShadow: POSTER_SHADOW,
          }}
        >
          <PosterArt title={title} />
        </div>
        <span
          style={{
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
            fontSize: 10,
            color: '#6b7280',
          }}
        >
          {title}
        </span>
      </div>
    ))}
  </div>
);
