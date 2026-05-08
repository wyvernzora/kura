import { memo } from 'react';

import { PALETTES, type Palette } from './palettes';
import { hashStr, pickGlyph, pickGlyph2 } from './text';

/*
 * Deterministic poster art generator.
 *
 * Same title → same hash → same palette + composition + glyphs, every
 * render. Compositions are flat SVG (no animation, no external assets)
 * so they bundle cheap, render fast, and read well over light + dark
 * surfaces.
 *
 * The 140×200 viewBox matches the prototype's 0.7 aspect; consumers
 * render the SVG into a Card body that owns the actual aspect ratio,
 * so this component just fills its container.
 */

interface CompositionProps {
  bg: string;
  accent: string;
  ink: string;
  paper: string;
  glyph: string;
  glyph2: string;
}

type Composition = (props: CompositionProps) => React.JSX.Element;

const CompBigGlyph: Composition = ({ bg, accent, paper, glyph }) => (
  <>
    <rect width="140" height="200" fill={bg} />
    <text
      x="50%"
      y="62%"
      textAnchor="middle"
      fill={paper}
      style={{
        fontFamily: "'Noto Serif JP', serif",
        fontSize: 150,
        fontWeight: 900,
        opacity: 0.92,
      }}
    >
      {glyph}
    </text>
    <rect x="0" y="170" width="140" height="6" fill={accent} />
  </>
);

const CompHorizon: Composition = ({ bg, accent, ink, paper, glyph }) => (
  <>
    <rect width="140" height="200" fill={bg} />
    <circle cx="70" cy="78" r="42" fill={accent} />
    <rect y="120" width="140" height="80" fill={ink} opacity="0.28" />
    <line x1="0" y1="120" x2="140" y2="120" stroke={ink} strokeWidth="1" />
    <line x1="0" y1="135" x2="140" y2="135" stroke={paper} strokeWidth="0.7" opacity="0.4" />
    <line x1="0" y1="148" x2="140" y2="148" stroke={paper} strokeWidth="0.7" opacity="0.3" />
    <line x1="0" y1="162" x2="140" y2="162" stroke={paper} strokeWidth="0.7" opacity="0.2" />
    <text
      x="10"
      y="186"
      fill={paper}
      style={{ fontFamily: "'Noto Serif JP', serif", fontSize: 18, fontWeight: 700 }}
    >
      {glyph}
    </text>
  </>
);

const CompBands: Composition = ({ bg, accent, ink, paper, glyph }) => (
  <>
    <rect width="140" height="200" fill={paper} />
    <rect y="0" width="140" height="74" fill={bg} />
    <rect y="74" width="140" height="20" fill={accent} />
    <rect y="94" width="140" height="106" fill={ink} />
    <text
      x="50%"
      y="124"
      textAnchor="middle"
      fill={paper}
      style={{
        fontFamily: "'Noto Serif JP', serif",
        fontSize: 28,
        fontWeight: 700,
        letterSpacing: -1,
      }}
    >
      {glyph}
    </text>
    <rect x="20" y="148" width="100" height="2" fill={paper} opacity="0.6" />
    <rect x="20" y="160" width="60" height="2" fill={paper} opacity="0.4" />
  </>
);

const CompPortrait: Composition = ({ bg, accent, ink, paper, glyph }) => (
  <>
    <rect width="140" height="200" fill={bg} />
    <rect x="0" y="155" width="140" height="45" fill={ink} />
    <circle cx="70" cy="80" r="46" fill={paper} />
    <circle cx="70" cy="80" r="46" fill="none" stroke={ink} strokeWidth="2" />
    <circle cx="70" cy="68" r="18" fill={ink} />
    <path d="M 36 130 Q 36 96 70 96 Q 104 96 104 130 L 104 126 L 36 126 Z" fill={ink} />
    <path d="M 52 60 Q 70 38 90 60 L 88 70 Q 70 58 52 70 Z" fill={accent} />
    <text
      x="50%"
      y="186"
      textAnchor="middle"
      fill={paper}
      style={{ fontFamily: "'Noto Serif JP', serif", fontSize: 18, fontWeight: 700 }}
    >
      {glyph}
    </text>
  </>
);

const CompStack: Composition = ({ bg, accent, ink, paper, glyph, glyph2 }) => (
  <>
    <rect width="140" height="200" fill={bg} />
    <rect x="0" y="0" width="40" height="200" fill={paper} />
    <text
      x="20"
      y="60"
      textAnchor="middle"
      fill={ink}
      style={{ fontFamily: "'Noto Serif JP', serif", fontSize: 28, fontWeight: 700 }}
    >
      {glyph}
    </text>
    {glyph2 && (
      <text
        x="20"
        y="100"
        textAnchor="middle"
        fill={ink}
        style={{ fontFamily: "'Noto Serif JP', serif", fontSize: 28, fontWeight: 700 }}
      >
        {glyph2}
      </text>
    )}
    <rect x="56" y="40" width="70" height="120" fill={accent} />
    <rect x="56" y="40" width="70" height="120" fill="none" stroke={ink} strokeWidth="1.5" />
    <line x1="56" y1="60" x2="126" y2="60" stroke={ink} strokeWidth="0.6" opacity="0.4" />
    <line x1="56" y1="80" x2="126" y2="80" stroke={ink} strokeWidth="0.6" opacity="0.4" />
    <line x1="56" y1="100" x2="126" y2="100" stroke={ink} strokeWidth="0.6" opacity="0.4" />
    <line x1="56" y1="120" x2="126" y2="120" stroke={ink} strokeWidth="0.6" opacity="0.4" />
    <line x1="56" y1="140" x2="126" y2="140" stroke={ink} strokeWidth="0.6" opacity="0.4" />
  </>
);

const COMPOSITIONS: readonly Composition[] = [
  CompBigGlyph,
  CompHorizon,
  CompBands,
  CompPortrait,
  CompStack,
];

interface PosterArtProps {
  title: string;
  className?: string;
}

function pickPalette(h: number): Palette {
  return PALETTES[h % PALETTES.length] as Palette;
}

function pickComposition(h: number): Composition {
  return COMPOSITIONS[(h >> 5) % COMPOSITIONS.length] as Composition;
}

/**
 * Renders the deterministic art for a title, scaled to fill its
 * container. Memoized on title so scrolling a virtualized grid
 * doesn't recompute palette + glyph lookups.
 */
export const PosterArt = memo(function PosterArt({ title, className }: PosterArtProps) {
  const safe = title ?? '';
  const h = hashStr(safe);
  const [bg, accent, ink, paper] = pickPalette(h);
  const Comp = pickComposition(h);
  const glyph = pickGlyph(safe);
  const glyph2 = pickGlyph2(safe);

  return (
    <svg
      role="presentation"
      aria-hidden="true"
      viewBox="0 0 140 200"
      preserveAspectRatio="xMidYMid slice"
      className={className}
      style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', display: 'block' }}
    >
      <Comp bg={bg} accent={accent} ink={ink} paper={paper} glyph={glyph} glyph2={glyph2} />
    </svg>
  );
});
