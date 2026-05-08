import { describe, expect, it } from 'vitest';

import { primaryStatus, secondaryStatus } from './status';

describe('primaryStatus', () => {
  it('returns the value for a single status', () => {
    expect(primaryStatus('airing')).toBe('airing');
    expect(primaryStatus('complete')).toBe('complete');
  });

  it('picks error first in any compound', () => {
    expect(primaryStatus(['airing', 'error'])).toBe('error');
    expect(primaryStatus(['error', 'incomplete'])).toBe('error');
  });

  it('picks incomplete over airing', () => {
    expect(primaryStatus(['airing', 'incomplete'])).toBe('incomplete');
  });

  it('picks airing over untracked / complete', () => {
    expect(primaryStatus(['airing', 'untracked'])).toBe('airing');
    expect(primaryStatus(['airing', 'complete'])).toBe('airing');
  });

  it('falls back to the first entry when none match priority', () => {
    expect(primaryStatus([] as never)).toBe('complete');
  });
});

describe('secondaryStatus', () => {
  it('returns undefined for a single status', () => {
    expect(secondaryStatus('airing')).toBeUndefined();
    expect(secondaryStatus(['airing'])).toBeUndefined();
  });

  it('returns the non-primary status from a compound', () => {
    expect(secondaryStatus(['airing', 'incomplete'])).toBe('airing');
    expect(secondaryStatus(['error', 'incomplete'])).toBe('incomplete');
  });
});
