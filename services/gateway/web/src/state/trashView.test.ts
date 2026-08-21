import { beforeEach, describe, expect, it } from 'vitest';

import { useTrashView } from './trashView';

describe('trash view store', () => {
  beforeEach(() => {
    useTrashView.getState().clear();
  });

  // Reads the store's declared initial state rather than the current
  // one: `beforeEach` calls clear(), so asserting getState() here would
  // only re-test clear() and would pass for any starting value.
  it('starts with no age filter and a size-descending sort', () => {
    expect(useTrashView.getInitialState().maxAgeHours).toBe(0);
    expect(useTrashView.getInitialState().sort).toEqual({ key: 'size', direction: 'desc' });
  });

  it('setMaxAgeHours narrows the view', () => {
    useTrashView.getState().setMaxAgeHours(168);
    expect(useTrashView.getState().maxAgeHours).toBe(168);
  });

  it('setSort replaces the whole spec', () => {
    useTrashView.getState().setSort({ key: 'title', direction: 'asc' });
    expect(useTrashView.getState().sort).toEqual({ key: 'title', direction: 'asc' });
  });

  it('clear returns both the age filter and the sort to the feature default', () => {
    useTrashView.getState().setMaxAgeHours(720);
    useTrashView.getState().setSort({ key: 'trashed', direction: 'asc' });
    useTrashView.getState().clear();
    expect(useTrashView.getState().maxAgeHours).toBe(0);
    expect(useTrashView.getState().sort).toEqual({ key: 'size', direction: 'desc' });
  });
});
