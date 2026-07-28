import type { Meta, StoryObj } from '@storybook/react';

import { CompanionsBadge } from './CompanionsBadge';

const meta: Meta<typeof CompanionsBadge> = {
  title: 'Series/CompanionsBadge',
  component: CompanionsBadge,
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj<typeof CompanionsBadge>;

/** Zero renders nothing — helps confirm the no-op branch is wired. */
export const ZeroRendersNothing: Story = {
  render: () => (
    <div className="flex items-center gap-2 text-sm text-ink">
      <span>S01E01 — Pilot</span>
      <CompanionsBadge count={0} />
      <span className="font-mono text-[10px] text-muted">(badge intentionally absent)</span>
    </div>
  ),
};

export const One: Story = {
  args: { count: 1 },
};

export const Three: Story = {
  args: { count: 3 },
};

export const Many: Story = {
  args: { count: 12 },
};

/** In context next to a title so the inline weight is reviewable. */
export const Inline: Story = {
  render: () => (
    <div className="flex items-center gap-1 text-sm text-ink">
      <span>S01E01 — 出会い</span>
      <CompanionsBadge count={3} />
    </div>
  ),
};
