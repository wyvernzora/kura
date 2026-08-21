import { beforeEach, describe, expect, it } from 'vitest';

import { useReleasesView } from './releasesView';

describe('releases view store', () => {
  beforeEach(() => {
    useReleasesView.getState().clear();
  });

  // Reads the declared initial state rather than the current one:
  // `beforeEach` calls clear(), so asserting getState() here would only
  // re-test clear().
  it('starts on the attention set — exhausted plus low confidence', () => {
    expect([...useReleasesView.getInitialState().active].sort()).toEqual(['exhausted', 'lowconf']);
  });

  it('toggle adds and removes a key', () => {
    useReleasesView.getState().toggle('suppressed');
    expect(useReleasesView.getState().active.has('suppressed')).toBe(true);
    useReleasesView.getState().toggle('suppressed');
    expect(useReleasesView.getState().active.has('suppressed')).toBe(false);
  });

  it('showAll empties the set, which the mapping reads as every status', () => {
    useReleasesView.getState().showAll();
    expect(useReleasesView.getState().active.size).toBe(0);
  });

  it('clear returns to the attention set from any other view', () => {
    useReleasesView.getState().showAll();
    useReleasesView.getState().toggle('dead');
    useReleasesView.getState().clear();
    expect([...useReleasesView.getState().active].sort()).toEqual(['exhausted', 'lowconf']);
  });
});
