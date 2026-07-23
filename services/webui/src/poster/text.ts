/**
 * Deterministic 32-bit-ish hash. Same string in always returns the
 * same number out, so a given title always picks the same palette
 * and composition.
 */
export function hashStr(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0;
  }
  return Math.abs(h);
}

const isCJK = (codepoint: number): boolean =>
  (codepoint >= 0x3400 && codepoint <= 0x9fff) || (codepoint >= 0x3040 && codepoint <= 0x30ff);

/**
 * Picks the most evocative single character for the poster glyph.
 * Prefers CJK ideographs / kana for anime titles; falls back to the
 * first Latin letter or digit; ultimately returns "?" for empty input.
 */
export function pickGlyph(title: string): string {
  if (!title) {
    return '?';
  }
  for (const ch of title) {
    const cp = ch.codePointAt(0);
    if (cp !== undefined && isCJK(cp)) {
      return ch;
    }
  }
  const m = title.match(/[A-Za-z0-9]/);
  if (m) {
    return m[0].toUpperCase();
  }
  return title[0] ?? '?';
}

/**
 * Second glyph, surfaced by stacked compositions. Returns empty
 * string when the title doesn't have a second CJK character.
 */
export function pickGlyph2(title: string): string {
  if (!title) {
    return '';
  }
  let count = 0;
  for (const ch of title) {
    const cp = ch.codePointAt(0);
    if (cp !== undefined && isCJK(cp)) {
      count += 1;
      if (count === 2) {
        return ch;
      }
    }
  }
  return '';
}
