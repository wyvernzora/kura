import { describe, expect, it } from 'vitest';

import { pickDensity } from './useAutoDensity';

describe('pickDensity', () => {
  it('xs for very narrow viewports', () => {
    const d = pickDensity(320);
    expect(d.breakpoint).toBe('xs');
    expect(d.minPoster).toBe(96);
    expect(d.gap).toBe(12);
    expect(d.rowGap).toBe(24);
    expect(d.dense).toBe(true);
  });

  it('xs at 0 (defensive)', () => {
    expect(pickDensity(0).breakpoint).toBe('xs');
  });

  it('sm just under the desktop threshold', () => {
    expect(pickDensity(720).breakpoint).toBe('sm');
    expect(pickDensity(720).minPoster).toBe(112);
    expect(pickDensity(720).gap).toBe(14);
    expect(pickDensity(720).rowGap).toBe(28);
  });

  it('md at the boundary 768', () => {
    expect(pickDensity(768).breakpoint).toBe('md');
    expect(pickDensity(768).gap).toBe(16);
    expect(pickDensity(768).rowGap).toBe(32);
    expect(pickDensity(768).dense).toBe(false);
  });

  it('lg at 1200 and above', () => {
    expect(pickDensity(1200).breakpoint).toBe('lg');
    expect(pickDensity(1920).breakpoint).toBe('lg');
    expect(pickDensity(1920).minPoster).toBe(160);
    expect(pickDensity(1920).gap).toBe(18);
    expect(pickDensity(1920).rowGap).toBe(34);
  });

  it('rowGap exceeds gap so rows get extra breathing room', () => {
    for (const w of [320, 720, 900, 1600]) {
      const d = pickDensity(w);
      expect(d.rowGap).toBeGreaterThan(d.gap);
    }
  });

  it('boundary at 480 picks sm, not xs', () => {
    expect(pickDensity(480).breakpoint).toBe('sm');
  });
});
