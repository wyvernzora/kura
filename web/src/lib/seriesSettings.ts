export type Priority = 'high' | 'normal' | 'low' | 'disabled';

export const PRIORITY_TAG: Record<Exclude<Priority, 'normal'>, string> = {
  high: 'priority:high',
  low: 'priority:low',
  disabled: 'maintenance:disabled',
};

const MANAGED_TAGS = [
  'priority:high',
  'priority:low',
  'maintenance:disabled',
  'maintenance:requested',
] as const;

export function priorityFromTags(tags: readonly string[]): Priority {
  if (tags.includes(PRIORITY_TAG.disabled)) {
    return 'disabled';
  }
  if (tags.includes(PRIORITY_TAG.high)) {
    return 'high';
  }
  if (tags.includes(PRIORITY_TAG.low)) {
    return 'low';
  }
  return 'normal';
}

export function maintenanceRequested(tags: string[]): boolean {
  return tags.includes('maintenance:requested');
}

export function desiredManagedTags(priority: Priority, requested: boolean): string[] {
  const tags: string[] = [];
  if (priority !== 'normal') {
    tags.push(PRIORITY_TAG[priority]);
  }
  if (requested && priority !== 'disabled') {
    tags.push('maintenance:requested');
  }
  return tags;
}

export function computeTagExpressions(
  current: string[],
  priority: Priority,
  requested: boolean,
): string[] {
  const desired = new Set(desiredManagedTags(priority, requested));

  const expressions: string[] = [];
  for (const tag of MANAGED_TAGS) {
    const present = current.includes(tag);
    if (desired.has(tag) && !present) {
      expressions.push(tag);
    } else if (!desired.has(tag) && present) {
      expressions.push(`!${tag}`);
    }
  }
  return expressions;
}
