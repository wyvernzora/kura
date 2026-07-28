import { describe, expect, it } from 'vitest';

import {
  computeTagExpressions,
  maintenanceRequested,
  type Priority,
  priorityFromTags,
} from './seriesSettings';

const PRIORITY_TAGS: Record<Priority, string[]> = {
  high: ['priority:high'],
  normal: [],
  low: ['priority:low'],
  disabled: ['maintenance:disabled'],
};

describe('priorityFromTags', () => {
  it.each([
    [['priority:high'], 'high'],
    [[], 'normal'],
    [['priority:low'], 'low'],
    [['maintenance:disabled'], 'disabled'],
  ] as const)('maps %j to %s', (tags, expected) => {
    expect(priorityFromTags([...tags])).toBe(expected);
  });

  it('gives maintenance:disabled precedence over priority tags', () => {
    expect(priorityFromTags(['priority:high', 'maintenance:disabled'])).toBe('disabled');
  });
});

describe('maintenanceRequested', () => {
  it('reports whether the request tag is present', () => {
    expect(maintenanceRequested(['maintenance:requested'])).toBe(true);
    expect(maintenanceRequested(['fansub:goodgroup'])).toBe(false);
  });
});

describe('computeTagExpressions', () => {
  it.each([
    ['high', 'high', []],
    ['high', 'normal', ['!priority:high']],
    ['high', 'low', ['!priority:high', 'priority:low']],
    ['high', 'disabled', ['!priority:high', 'maintenance:disabled']],
    ['normal', 'high', ['priority:high']],
    ['normal', 'normal', []],
    ['normal', 'low', ['priority:low']],
    ['normal', 'disabled', ['maintenance:disabled']],
    ['low', 'high', ['priority:high', '!priority:low']],
    ['low', 'normal', ['!priority:low']],
    ['low', 'low', []],
    ['low', 'disabled', ['!priority:low', 'maintenance:disabled']],
    ['disabled', 'high', ['priority:high', '!maintenance:disabled']],
    ['disabled', 'normal', ['!maintenance:disabled']],
    ['disabled', 'low', ['priority:low', '!maintenance:disabled']],
    ['disabled', 'disabled', []],
  ] satisfies Array<[Priority, Priority, string[]]>)('%s → %s', (from, to, expected) => {
    expect(computeTagExpressions(PRIORITY_TAGS[from], to, false)).toEqual(expected);
  });

  it('forces a maintenance request off when priority becomes disabled', () => {
    expect(computeTagExpressions(['maintenance:requested'], 'disabled', true)).toEqual([
      'maintenance:disabled',
      '!maintenance:requested',
    ]);
  });

  it('clears conflicting disabled and requested tags when returning to normal', () => {
    expect(
      computeTagExpressions(['maintenance:disabled', 'maintenance:requested'], 'normal', false),
    ).toEqual(['!maintenance:disabled', '!maintenance:requested']);
  });

  it('returns an empty diff when the selection is unchanged', () => {
    expect(
      computeTagExpressions(
        ['fansub:goodgroup', 'priority:low', 'maintenance:requested'],
        'low',
        true,
      ),
    ).toEqual([]);
  });

  it('never includes non-managed tags in expressions', () => {
    const expressions = computeTagExpressions(
      ['fansub:goodgroup', 'provider:tvdb', 'priority:low'],
      'high',
      false,
    );
    expect(expressions).toEqual(['priority:high', '!priority:low']);
    expect(expressions.every((expression) => !expression.includes('fansub:goodgroup'))).toBe(true);
    expect(expressions.every((expression) => !expression.includes('provider:tvdb'))).toBe(true);
  });

  it('toggles maintenance requests on and off', () => {
    expect(computeTagExpressions([], 'normal', true)).toEqual(['maintenance:requested']);
    expect(computeTagExpressions(['maintenance:requested'], 'normal', false)).toEqual([
      '!maintenance:requested',
    ]);
  });
});
