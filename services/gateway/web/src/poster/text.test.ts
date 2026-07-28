import { describe, expect, it } from 'vitest';

import { hashStr, pickGlyph, pickGlyph2 } from './text';

describe('hashStr', () => {
  it('returns the same hash for the same input', () => {
    expect(hashStr('Frieren')).toBe(hashStr('Frieren'));
  });

  it('returns different hashes for different inputs', () => {
    expect(hashStr('A')).not.toBe(hashStr('B'));
  });

  it('returns a non-negative integer', () => {
    expect(hashStr('葬送のフリーレン')).toBeGreaterThanOrEqual(0);
  });

  it('handles empty input', () => {
    expect(hashStr('')).toBe(0);
  });
});

describe('pickGlyph', () => {
  it('prefers the first CJK character', () => {
    expect(pickGlyph('葬送のフリーレン')).toBe('葬');
  });

  it('falls back to the first uppercased Latin letter', () => {
    expect(pickGlyph('frieren beyond journey')).toBe('F');
  });

  it('uppercases lowercase ASCII', () => {
    expect(pickGlyph('the boy and the heron')).toBe('T');
  });

  it('returns "?" for empty input', () => {
    expect(pickGlyph('')).toBe('?');
  });
});

describe('pickGlyph2', () => {
  it('returns the second CJK character when available', () => {
    expect(pickGlyph2('葬送のフリーレン')).toBe('送');
  });

  it('returns empty string when fewer than two CJK characters', () => {
    expect(pickGlyph2('Frieren')).toBe('');
    expect(pickGlyph2('葬')).toBe('');
  });

  it('returns empty string for empty input', () => {
    expect(pickGlyph2('')).toBe('');
  });
});
