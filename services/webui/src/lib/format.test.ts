import { describe, expect, it } from 'vitest';

import { formatDateTime, formatSize } from './format';

describe('formatSize', () => {
  it('formats bytes below one KiB', () => {
    expect(formatSize(1023)).toBe('1023 B');
  });

  it('rounds KiB values to whole kilobytes', () => {
    expect(formatSize(1024)).toBe('1 KB');
    expect(formatSize(58_000)).toBe('57 KB');
  });

  it('rounds MiB values to whole megabytes', () => {
    expect(formatSize(1.6 * 1024 ** 2)).toBe('2 MB');
  });

  it('formats GiB values with two decimal places', () => {
    expect(formatSize(1.42 * 1024 ** 3)).toBe('1.42 GB');
  });
});

describe('formatDateTime', () => {
  it('formats an absolute local date and time', () => {
    expect(formatDateTime('2026-07-17T18:40:00')).toBe('Jul 17, 2026, 06:40 PM');
  });

  it('returns null for absent or invalid timestamps', () => {
    expect(formatDateTime(undefined)).toBeNull();
    expect(formatDateTime('not-a-date')).toBeNull();
  });
});
