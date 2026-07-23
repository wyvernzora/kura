import { describe, expect, it } from 'vitest';

import { formatRelativeAgo } from './relativeTime';

const NOW = new Date('2026-05-07T12:00:00Z');

describe('formatRelativeAgo', () => {
  it('empty string for missing input', () => {
    expect(formatRelativeAgo(undefined, NOW)).toBe('');
    expect(formatRelativeAgo('', NOW)).toBe('');
  });

  it('empty string for unparseable input', () => {
    expect(formatRelativeAgo('not-a-date', NOW)).toBe('');
  });

  it('"just now" under one minute', () => {
    const t = new Date(NOW.getTime() - 5_000).toISOString();
    expect(formatRelativeAgo(t, NOW)).toBe('just now');
  });

  it('boundary at 1 minute flips from "just now" to "1m ago"', () => {
    const justUnder = new Date(NOW.getTime() - 59_999).toISOString();
    const justOver = new Date(NOW.getTime() - 60_000).toISOString();
    expect(formatRelativeAgo(justUnder, NOW)).toBe('just now');
    expect(formatRelativeAgo(justOver, NOW)).toBe('1m ago');
  });

  it('"Nm ago" under one hour', () => {
    const t = new Date(NOW.getTime() - 23 * 60_000).toISOString();
    expect(formatRelativeAgo(t, NOW)).toBe('23m ago');
  });

  it('boundary at 60 minutes flips from minutes to hours', () => {
    const justUnder = new Date(NOW.getTime() - 59 * 60_000).toISOString();
    const at60 = new Date(NOW.getTime() - 60 * 60_000).toISOString();
    expect(formatRelativeAgo(justUnder, NOW)).toBe('59m ago');
    expect(formatRelativeAgo(at60, NOW)).toBe('1h ago');
  });

  it('"Nh ago" under one day', () => {
    const t = new Date(NOW.getTime() - 5 * 60 * 60_000).toISOString();
    expect(formatRelativeAgo(t, NOW)).toBe('5h ago');
  });

  it('boundary at 24 hours flips from hours to days', () => {
    const justUnder = new Date(NOW.getTime() - 23 * 60 * 60_000).toISOString();
    const at24 = new Date(NOW.getTime() - 24 * 60 * 60_000).toISOString();
    expect(formatRelativeAgo(justUnder, NOW)).toBe('23h ago');
    expect(formatRelativeAgo(at24, NOW)).toBe('1d ago');
  });

  it('"Nd ago" for multi-day', () => {
    const t = new Date(NOW.getTime() - 14 * 24 * 60 * 60_000).toISOString();
    expect(formatRelativeAgo(t, NOW)).toBe('14d ago');
  });
});
