import { beforeEach, describe, expect, it } from 'vitest';

import { useSearch } from './search';

describe('search store scope', () => {
  beforeEach(() => {
    useSearch.getState().clear();
  });

  it('starts in library scope', () => {
    expect(useSearch.getState().scope).toBe('library');
  });

  it('setScope switches to tvdb', () => {
    useSearch.getState().setQuery('frieren');
    useSearch.getState().setScope('tvdb');
    expect(useSearch.getState().scope).toBe('tvdb');
  });

  it('forces library scope when the query drops below the TVDB floor', () => {
    useSearch.getState().setQuery('frieren');
    useSearch.getState().setScope('tvdb');
    useSearch.getState().setQuery('fr');
    expect(useSearch.getState().scope).toBe('library');
  });

  it('keeps tvdb scope while the query stays at or above the floor', () => {
    useSearch.getState().setQuery('frieren');
    useSearch.getState().setScope('tvdb');
    useSearch.getState().setQuery('frie');
    expect(useSearch.getState().scope).toBe('tvdb');
  });

  it('whitespace does not count toward the floor', () => {
    useSearch.getState().setQuery('frieren');
    useSearch.getState().setScope('tvdb');
    useSearch.getState().setQuery('a  ');
    expect(useSearch.getState().scope).toBe('library');
  });

  it('clear resets scope along with the query', () => {
    useSearch.getState().setQuery('frieren');
    useSearch.getState().setScope('tvdb');
    useSearch.getState().clear();
    expect(useSearch.getState().scope).toBe('library');
    expect(useSearch.getState().query).toBe('');
  });
});
